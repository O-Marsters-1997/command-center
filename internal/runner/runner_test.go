package runner_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/runner"
)

// commitsScript is the absolute path to testdata/agents/commits.sh (the fake agent that
// commits a file and exits 0), which lives at the module root.
func commitsScript(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs("../../testdata/agents/commits.sh")
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// gitRepo creates a real git repo at a temp dir so commits.sh has something to commit into.
// The git identity is set via t.Setenv, not just on the setup commands' own exec.Cmd, because
// Spawn's env is os.Environ() minus the API key — it must inherit the same identity commits.sh
// needs to `git commit` inside the spawned process.
func gitRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("GIT_AUTHOR_NAME", "Command Centre")
	t.Setenv("GIT_AUTHOR_EMAIL", "cc@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Command Centre")
	t.Setenv("GIT_COMMITTER_EMAIL", "cc@example.com")

	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = os.Environ()
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("commit", "-q", "--allow-empty", "-m", "initial")
	return dir
}

// waitForFile polls for path to exist, failing the test after a short deadline. Spawn does not
// wait on the process, so the test must poll for the side effect it produces instead.
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

// reapExit blocks until a spawned agent has exited, asserting it exited cleanly. Every Spawn
// test must call it before returning: the spawned process writes inside a t.TempDir, and Go
// fires that directory's RemoveAll at cleanup — a child still running there fails the removal,
// and testing.T reports that against the whole package rather than this one test.
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

// spawnCase describes one ProcessRunner.Spawn run. script is the body of a throwaway agent that
// writes its observation to dump; command, when set, replaces the script as AgentCommand[0].
type spawnCase struct {
	name        string
	script      string
	command     []string
	configure   func(t *testing.T, cfg *runner.SpawnConfig)
	dumpOutside bool
	check       func(t *testing.T, cfg runner.SpawnConfig, pid int, dump string)
}

func TestProcessRunnerSpawn(t *testing.T) {
	writeFile := func(t *testing.T, content string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "input")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
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
				cfg.WorktreePath = gitRepo(t)
				cfg.SettingsPath = writeFile(t, "{}")
				cfg.PromptPath = writeFile(t, "/implement sandbox://CC-1")
				t.Setenv("ANTHROPIC_API_KEY", "test-secret-key")
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
					t.Fatal(err)
				}
				if len(logged) != 0 {
					t.Errorf("commits.sh wrote no stdout/stderr, but the log file has content: %q", logged)
				}
			},
		},
		{
			name:   "strips ANTHROPIC_API_KEY from the environment",
			script: "#!/bin/sh\nenv > $DUMP\n",
			configure: func(t *testing.T, _ *runner.SpawnConfig) {
				t.Helper()
				t.Setenv("ANTHROPIC_API_KEY", "test-secret-key")
			},
			check: func(t *testing.T, _ runner.SpawnConfig, _ int, dump string) {
				t.Helper()
				if strings.Contains(dump, "ANTHROPIC_API_KEY=") {
					t.Errorf("spawned process environment still carries ANTHROPIC_API_KEY:\n%s", dump)
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
			name:    "substitutes the agents path into argv",
			script:  dumpArg,
			command: []string{"{agents}"},
			configure: func(t *testing.T, cfg *runner.SpawnConfig) {
				t.Helper()
				cfg.AgentsPath = writeFile(t, "{}")
			},
			check: wantArgv(func(cfg runner.SpawnConfig) string { return cfg.AgentsPath }),
		},
		{
			name:    "drops the agents flag when the path is empty",
			script:  dumpArgs,
			command: []string{"before", "--agents", "{agents}", "after"},
			check:   wantArgv(func(runner.SpawnConfig) string { return "before after" }),
		},
		{
			name:    "drops the system prompt flag when the path is empty",
			script:  dumpArgs,
			command: []string{"before", "--append-system-prompt-file", "{system_prompt}", "after"},
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
					t.Fatal(err)
				}
				got, err := filepath.EvalSymlinks(strings.TrimSpace(dump))
				if err != nil {
					t.Fatal(err)
				}
				if got != want {
					t.Errorf("agent ran in %q, want the worktree %q", got, want)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := runner.SpawnConfig{
				WorktreePath: t.TempDir(),
				SettingsPath: filepath.Join(t.TempDir(), "agent.json"),
				PromptPath:   writeFile(t, "prompt"),
			}
			logFile, err := os.Create(filepath.Join(t.TempDir(), "run.jsonl"))
			if err != nil {
				t.Fatal(err)
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
					t.Fatal(err)
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
