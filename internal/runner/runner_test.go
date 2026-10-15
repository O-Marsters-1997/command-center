package runner_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/gittest"
	"github.com/O-Marsters-1997/command-center/internal/runner"
)

func commitsScript(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs("../../testdata/agents/commits.sh")
	if err != nil {
		t.Fatalf("Abs(commits.sh): %v", err)
	}
	return abs
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	exists := func() bool {
		_, err := os.Stat(path)
		return err == nil
	}
	if !pollUntil(5*time.Second, exists) {
		t.Fatalf("%s did not appear within the deadline", path)
	}
}

func pollUntil(within time.Duration, done func() bool) bool {
	poll := time.NewTicker(10 * time.Millisecond)
	defer poll.Stop()
	deadline := time.After(within)
	for !done() {
		select {
		case <-poll.C:
		case <-deadline:
			return false
		}
	}
	return true
}

func reapExit(t *testing.T, pid int) {
	t.Helper()
	exitCode, ok := runner.ProcessRunner{}.Reap(pid)
	if !ok {
		t.Fatalf("Reap reported no exit code for pid %d, which is our own direct child", pid)
	}
	if exitCode != 0 {
		t.Fatalf("spawned agent exited %d, want 0", exitCode)
	}
}

type spawnCase struct {
	name        string
	script      string
	command     []string
	configure   func(t *testing.T, cfg *runner.SpawnConfig)
	dumpOutside bool
	check       func(t *testing.T, cfg runner.SpawnConfig, pid int, dump string)
}

func TestProcessRunnerSpawn(t *testing.T) {
	t.Parallel()
	writeFile := func(t *testing.T, content string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "input")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("WriteFile(input): %v", err)
		}
		return path
	}
	wantArgv := func(want func(cfg runner.SpawnConfig) string) func(*testing.T, runner.SpawnConfig, int, string) {
		return func(t *testing.T, cfg runner.SpawnConfig, _ int, dump string) {
			t.Helper()
			if got := want(cfg); dump != got {
				t.Errorf("agent received argv %q, want %q", dump, got)
			}
		}
	}
	dumpArg := "#!/bin/sh\nprintf '%s' \"$1\" > $DUMP\n"
	dumpArgs := "#!/bin/sh\nprintf '%s' \"$*\" > $DUMP\n"
	const prompt = "/implement sandbox://CC-1\n\n## Ticket\n\nMake it work."

	tests := []spawnCase{
		{
			name:    "substitutes the worktree, settings and prompt file into argv and commits in the worktree",
			command: []string{commitsScript(t), "{worktree}", "{settings}", "{prompt_file}"},
			configure: func(t *testing.T, cfg *runner.SpawnConfig) {
				t.Helper()
				cfg.WorktreePath = gittest.Init(t)
				cfg.SettingsPath = writeFile(t, "{}")
				cfg.PromptPath = writeFile(t, "/implement sandbox://CC-1")
			},
			check: func(t *testing.T, cfg runner.SpawnConfig, _ int, _ string) {
				t.Helper()
				out, err := exec.Command("git", "-C", cfg.WorktreePath, "log", "--oneline").CombinedOutput()
				if err != nil {
					t.Fatalf("git log: %v: %s", err, out)
				}
				if !strings.Contains(string(out), "commit from commits.sh") {
					t.Errorf("git log = %q, want the agent's commit", out)
				}
				logged, err := os.ReadFile(cfg.LogFile.Name())
				if err != nil {
					t.Fatalf("ReadFile(log): %v", err)
				}
				if len(logged) != 0 {
					t.Errorf("commits.sh wrote no stdout/stderr, but the log file has content: %q", logged)
				}
			},
		},
		{
			name:   "makes the agent its own process group leader",
			script: "#!/bin/sh\nps -o pgid= -p $$ > $DUMP\n",
			check: func(t *testing.T, _ runner.SpawnConfig, pid int, dump string) {
				t.Helper()
				gotPgid, err := strconv.Atoi(strings.TrimSpace(dump))
				if err != nil {
					t.Fatalf("parse recorded pgid %q: %v", dump, err)
				}
				if gotPgid != pid {
					t.Errorf("pgid = %d, want the leader's own pid %d", gotPgid, pid)
				}
			},
		},
		{
			name:      "substitutes the prompt text into argv",
			script:    dumpArg,
			command:   []string{"{prompt}"},
			configure: func(_ *testing.T, cfg *runner.SpawnConfig) { cfg.Prompt = prompt },
			check:     wantArgv(func(runner.SpawnConfig) string { return prompt }),
		},
		{
			name:      "substitutes the model into argv",
			script:    dumpArg,
			command:   []string{"{model}"},
			configure: func(_ *testing.T, cfg *runner.SpawnConfig) { cfg.Model = "claude-haiku-5-5" },
			check:     wantArgv(func(runner.SpawnConfig) string { return "claude-haiku-5-5" }),
		},
		{
			name:    "substitutes the system prompt path into argv",
			script:  dumpArg,
			command: []string{"{system_prompt}"},
			configure: func(t *testing.T, cfg *runner.SpawnConfig) {
				t.Helper()
				cfg.SystemPromptPath = writeFile(t, "{}")
			},
			check: wantArgv(func(cfg runner.SpawnConfig) string { return cfg.SystemPromptPath }),
		},
		{
			name:    "drops the system prompt flag when the path is empty",
			script:  dumpArgs,
			command: []string{"before", "--append-system-prompt-file", "{system_prompt}", "after"},
			check:   wantArgv(func(runner.SpawnConfig) string { return "before after" }),
		},
		{
			name:    "expands the session element to --session-id for a new session",
			script:  dumpArgs,
			command: []string{"before", "{session}", "after"},
			configure: func(_ *testing.T, cfg *runner.SpawnConfig) {
				cfg.SessionID = "4e1c0a52-0000-4000-8000-000000000001"
			},
			check: wantArgv(func(runner.SpawnConfig) string {
				return "before --session-id 4e1c0a52-0000-4000-8000-000000000001 after"
			}),
		},
		{
			name:    "expands the session element to --resume for a resumed session",
			script:  dumpArgs,
			command: []string{"before", "{session}", "after"},
			configure: func(_ *testing.T, cfg *runner.SpawnConfig) {
				cfg.SessionID = "4e1c0a52-0000-4000-8000-000000000001"
				cfg.Resume = true
			},
			check: wantArgv(func(runner.SpawnConfig) string {
				return "before --resume 4e1c0a52-0000-4000-8000-000000000001 after"
			}),
		},
		{
			name:    "drops the session element when there is no session",
			script:  dumpArgs,
			command: []string{"before", "{session}", "after"},
			check:   wantArgv(func(runner.SpawnConfig) string { return "before after" }),
		},
		{
			name:        "runs in the worktree even when argv never mentions it",
			script:      "#!/bin/sh\npwd > $DUMP\n",
			dumpOutside: true,
			check: func(t *testing.T, cfg runner.SpawnConfig, _ int, dump string) {
				t.Helper()
				want, err := filepath.EvalSymlinks(cfg.WorktreePath)
				if err != nil {
					t.Fatalf("EvalSymlinks(worktree): %v", err)
				}
				got, err := filepath.EvalSymlinks(strings.TrimSpace(dump))
				if err != nil {
					t.Fatalf("EvalSymlinks(dump): %v", err)
				}
				if got != want {
					t.Errorf("agent ran in %q, want the worktree %q", got, want)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := runner.SpawnConfig{
				WorktreePath: t.TempDir(),
				SettingsPath: filepath.Join(t.TempDir(), "agent.json"),
				PromptPath:   writeFile(t, "prompt"),
			}
			logFile, err := os.Create(filepath.Join(t.TempDir(), "run.jsonl"))
			if err != nil {
				t.Fatalf("Create(run log): %v", err)
			}
			t.Cleanup(func() { _ = logFile.Close() })
			cfg.LogFile = logFile

			if tt.configure != nil {
				tt.configure(t, &cfg)
			}
			dump := filepath.Join(cfg.WorktreePath, "dump.txt")
			if tt.dumpOutside {
				dump = filepath.Join(t.TempDir(), "dump.txt")
			}
			if tt.script != "" {
				scriptPath := filepath.Join(t.TempDir(), "agent.sh")
				body := strings.ReplaceAll(tt.script, "$DUMP", dump)
				if err := os.WriteFile(scriptPath, []byte(body), 0o755); err != nil {
					t.Fatalf("WriteFile(agent script): %v", err)
				}
				cfg.AgentCommand = append([]string{scriptPath}, tt.command...)
			} else {
				cfg.AgentCommand = tt.command
			}

			result, err := (runner.ProcessRunner{}).Spawn(t.Context(), cfg)
			if err != nil {
				t.Fatalf("Spawn: %v", err)
			}
			if result.Pid == 0 {
				t.Fatal("Spawn returned a zero pid")
			}
			reapExit(t, result.Pid)

			var got string
			if tt.script != "" {
				raw, err := os.ReadFile(dump)
				if err != nil {
					t.Fatalf("the agent script never wrote its observation to $DUMP: %v", err)
				}
				got = string(raw)
			}
			tt.check(t, cfg, result.Pid, got)
		})
	}
}

func TestProcessRunnerSpawnStripsSecret(t *testing.T) {
	t.Parallel()
	if os.Getenv("RUNNER_SECRET_CHILD") != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestProcessRunnerSpawnStripsSecret$")
		cmd.Env = append(os.Environ(), "RUNNER_SECRET_CHILD=1", "ANTHROPIC_API_KEY=test-secret-key")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("Spawn with secret: %v: %s", err, out)
		}
		return
	}
	if os.Getenv("ANTHROPIC_API_KEY") != "test-secret-key" {
		t.Fatal("secret fixture was not installed in the isolated process")
	}
	worktree := t.TempDir()
	dump := filepath.Join(worktree, "env.txt")
	script := filepath.Join(t.TempDir(), "agent.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nenv > \"$1\"\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(agent script): %v", err)
	}
	logFile, err := os.Create(filepath.Join(t.TempDir(), "run.jsonl"))
	if err != nil {
		t.Fatalf("Create(run log): %v", err)
	}
	t.Cleanup(func() {
		if err := logFile.Close(); err != nil {
			t.Errorf("close run log: %v", err)
		}
	})
	cfg := runner.SpawnConfig{WorktreePath: worktree, AgentCommand: []string{script, dump}, LogFile: logFile}
	result, err := (runner.ProcessRunner{}).Spawn(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	reapExit(t, result.Pid)
	got, err := os.ReadFile(dump)
	if err != nil {
		t.Fatalf("read spawned environment: %v", err)
	}
	if strings.Contains(string(got), "ANTHROPIC_API_KEY=") {
		t.Errorf("spawned process environment still carries ANTHROPIC_API_KEY:\n%s", got)
	}
}
