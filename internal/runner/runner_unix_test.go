//go:build unix

package runner_test

import (
	"bufio"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/gittest"
	"github.com/O-Marsters-1997/command-center/internal/runner"
	"github.com/O-Marsters-1997/command-center/internal/runner/runnertest"
)

func writeFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "input")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(input): %v", err)
	}
	return path
}

func agentScript(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatalf("WriteFile(agent script): %v", err)
	}
	return path
}

func logFile(t *testing.T) *os.File {
	t.Helper()
	f, err := os.Create(filepath.Join(t.TempDir(), "run.jsonl"))
	if err != nil {
		t.Fatalf("Create(run log): %v", err)
	}
	t.Cleanup(func() {
		if err := f.Close(); err != nil {
			t.Errorf("close run log: %v", err)
		}
	})
	return f
}

func spawnConfig(t *testing.T, command ...string) runner.SpawnConfig {
	t.Helper()
	return runner.SpawnConfig{
		AgentCommand: command,
		WorktreePath: t.TempDir(),
		SettingsPath: filepath.Join(t.TempDir(), "agent.json"),
		PromptPath:   writeFile(t, "prompt"),
		LogFile:      logFile(t),
	}
}

func spawnToExit(t *testing.T, cfg runner.SpawnConfig) int {
	t.Helper()
	result, err := (runner.ProcessRunner{}).Spawn(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	exitCode, ok := (runner.ProcessRunner{}).Reap(result.Pid)
	if !ok || exitCode != 0 {
		t.Fatalf("Reap(%d) = %d, %v, want a clean exit of our own child", result.Pid, exitCode, ok)
	}
	return result.Pid
}

func readDump(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the agent never wrote its observation: %v", err)
	}
	return string(raw)
}

func TestProcessRunner_Spawn(t *testing.T) {
	t.Parallel()
	const session = "4e1c0a52-0000-4000-8000-000000000001"
	const prompt = "/implement sandbox://CC-1\n\n## Ticket\n\nMake it work."

	argvTests := []struct {
		name      string
		command   []string
		configure func(t *testing.T, cfg *runner.SpawnConfig)
		want      func(cfg runner.SpawnConfig) string
	}{
		{
			name:      "substitutes the prompt text",
			command:   []string{"{prompt}"},
			configure: func(_ *testing.T, cfg *runner.SpawnConfig) { cfg.Prompt = prompt },
			want:      func(runner.SpawnConfig) string { return prompt },
		},
		{
			name:      "substitutes the model",
			command:   []string{"{model}"},
			configure: func(_ *testing.T, cfg *runner.SpawnConfig) { cfg.Model = "claude-haiku-5-5" },
			want:      func(runner.SpawnConfig) string { return "claude-haiku-5-5" },
		},
		{
			name:    "substitutes the system prompt path",
			command: []string{"{system_prompt}"},
			configure: func(t *testing.T, cfg *runner.SpawnConfig) {
				t.Helper()
				cfg.SystemPromptPath = writeFile(t, "{}")
			},
			want: func(cfg runner.SpawnConfig) string { return cfg.SystemPromptPath },
		},
		{
			name:    "drops the system prompt flag when the path is empty",
			command: []string{"before", "--append-system-prompt-file", "{system_prompt}", "after"},
			want:    func(runner.SpawnConfig) string { return "before after" },
		},
		{
			name:      "expands the session element to --session-id for a new session",
			command:   []string{"before", "{session}", "after"},
			configure: func(_ *testing.T, cfg *runner.SpawnConfig) { cfg.SessionID = session },
			want:      func(runner.SpawnConfig) string { return "before --session-id " + session + " after" },
		},
		{
			name:    "expands the session element to --resume for a resumed session",
			command: []string{"before", "{session}", "after"},
			configure: func(_ *testing.T, cfg *runner.SpawnConfig) {
				cfg.SessionID = session
				cfg.Resume = true
			},
			want: func(runner.SpawnConfig) string { return "before --resume " + session + " after" },
		},
		{
			name:    "drops the session element when there is no session",
			command: []string{"before", "{session}", "after"},
			want:    func(runner.SpawnConfig) string { return "before after" },
		},
	}
	for _, tt := range argvTests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dump := filepath.Join(t.TempDir(), "argv.txt")
			cfg := spawnConfig(t, append([]string{agentScript(t, `printf '%s' "$*" > `+dump+"\n")}, tt.command...)...)
			if tt.configure != nil {
				tt.configure(t, &cfg)
			}

			spawnToExit(t, cfg)

			if got, want := readDump(t, dump), tt.want(cfg); got != want {
				t.Errorf("Spawn(%q) passed argv %q, want %q", tt.command, got, want)
			}
		})
	}

	t.Run("substitutes worktree, settings and prompt file, and the agent commits in the worktree", func(t *testing.T) {
		t.Parallel()
		script, err := filepath.Abs("../../testdata/agents/commits.sh")
		if err != nil {
			t.Fatalf("Abs(commits.sh): %v", err)
		}
		cfg := spawnConfig(t, script, "{worktree}", "{settings}", "{prompt_file}")
		cfg.WorktreePath = gittest.Init(t)
		cfg.SettingsPath = writeFile(t, "{}")
		cfg.PromptPath = writeFile(t, "/implement sandbox://CC-1")

		spawnToExit(t, cfg)

		out, err := exec.Command("git", "-C", cfg.WorktreePath, "log", "--oneline").CombinedOutput()
		if err != nil {
			t.Fatalf("git log: %v: %s", err, out)
		}
		if !strings.Contains(string(out), "commit from commits.sh") {
			t.Errorf("git log = %q, want the agent's commit", out)
		}
		if logged := readDump(t, cfg.LogFile.Name()); logged != "" {
			t.Errorf("run log = %q, want empty for an agent that writes nothing", logged)
		}
	})

	t.Run("makes the agent its own process group leader", func(t *testing.T) {
		t.Parallel()
		dump := filepath.Join(t.TempDir(), "pgid.txt")
		cfg := spawnConfig(t, agentScript(t, "ps -o pgid= -p $$ > "+dump+"\n"))

		pid := spawnToExit(t, cfg)

		got, err := strconv.Atoi(strings.TrimSpace(readDump(t, dump)))
		if err != nil {
			t.Fatalf("parse recorded pgid: %v", err)
		}
		if got != pid {
			t.Errorf("Spawn() pgid = %d, want the leader's own pid %d", got, pid)
		}
	})

	t.Run("runs in the worktree", func(t *testing.T) {
		t.Parallel()
		dump := filepath.Join(t.TempDir(), "pwd.txt")
		cfg := spawnConfig(t, agentScript(t, "pwd > "+dump+"\n"))

		spawnToExit(t, cfg)

		want, err := filepath.EvalSymlinks(cfg.WorktreePath)
		if err != nil {
			t.Fatalf("EvalSymlinks(worktree): %v", err)
		}
		got, err := filepath.EvalSymlinks(strings.TrimSpace(readDump(t, dump)))
		if err != nil {
			t.Fatalf("EvalSymlinks(dump): %v", err)
		}
		if got != want {
			t.Errorf("Spawn() ran in %q, want the worktree %q", got, want)
		}
	})

	t.Run("strips ANTHROPIC_API_KEY", func(t *testing.T) {
		t.Parallel()
		if os.Getenv("RUNNER_SECRET_CHILD") != "1" {
			cmd := exec.Command(os.Args[0], "-test.run=^TestProcessRunner_Spawn$/^strips_ANTHROPIC_API_KEY$")
			cmd.Env = append(os.Environ(), "RUNNER_SECRET_CHILD=1", "ANTHROPIC_API_KEY=test-secret-key")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("Spawn with secret: %v: %s", err, out)
			}
			return
		}
		if os.Getenv("ANTHROPIC_API_KEY") != "test-secret-key" {
			t.Fatal("secret fixture was not installed in the isolated process")
		}
		dump := filepath.Join(t.TempDir(), "env.txt")
		cfg := spawnConfig(t, agentScript(t, "env > "+dump+"\n"))

		spawnToExit(t, cfg)

		if got := readDump(t, dump); strings.Contains(got, "ANTHROPIC_API_KEY=") {
			t.Errorf("Spawn() environment carries ANTHROPIC_API_KEY:\n%s", got)
		}
	})

	t.Run("errors", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name    string
			command []string
		}{
			{name: "empty agent command", command: nil},
			{name: "a command that builds an empty argv", command: []string{"{session}"}},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				cfg := runner.SpawnConfig{AgentCommand: tt.command, WorktreePath: t.TempDir()}
				if _, err := (runner.ProcessRunner{}).Spawn(t.Context(), cfg); err == nil {
					t.Errorf("Spawn(%q) error = nil, want one", tt.command)
				}
			})
		}
	})
}

type processTable map[int]runner.Process

func (p processTable) Lookup(pid int) (runner.Process, bool, error) {
	proc, ok := p[pid]
	return proc, ok, nil
}

type unreadableTable struct{}

var errTable = errors.New("process table unreadable")

func (unreadableTable) Lookup(int) (runner.Process, bool, error) {
	return runner.Process{}, false, errTable
}

func startRealProcess(t *testing.T) (pid int, startedAt time.Time) {
	t.Helper()
	cmd := exec.Command("sleep", "30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start real process: %v", err)
	}
	startedAt = time.Now()
	t.Cleanup(func() {
		if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			t.Errorf("Kill(sleep): %v", err)
		}
		if err := cmd.Wait(); err != nil {
			if _, ok := errors.AsType[*exec.ExitError](err); !ok {
				t.Errorf("Wait(sleep): %v", err)
			}
		}
	})
	return cmd.Process.Pid, startedAt
}

func TestProcessRunner_Liveness(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	const pid = 4242

	tests := []struct {
		name      string
		table     processTable
		wantStart time.Time
		want      bool
	}{
		{
			name:      "running with a matching start time",
			table:     processTable{pid: {Elapsed: time.Minute}},
			wantStart: now.Add(-time.Minute),
			want:      true,
		},
		{
			name:      "start time within the pid-reuse tolerance",
			table:     processTable{pid: {Elapsed: time.Minute}},
			wantStart: now.Add(-time.Minute + 5*time.Second),
			want:      true,
		},
		{
			name:      "start time past the pid-reuse tolerance reads as a reused pid",
			table:     processTable{pid: {Elapsed: time.Minute}},
			wantStart: now.Add(-time.Minute + 6*time.Second),
			want:      false,
		},
		{
			name:      "zombie",
			table:     processTable{pid: {Zombie: true}},
			wantStart: now,
			want:      false,
		},
		{
			name:      "not in the table",
			table:     processTable{},
			wantStart: now,
			want:      false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := runner.ProcessRunner{Processes: tt.table}

			got, err := r.Liveness(pid, tt.wantStart, now)
			if err != nil {
				t.Fatalf("Liveness() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("Liveness(%d, %s, %s) = %v, want %v", pid, tt.wantStart, now, got, tt.want)
			}
		})
	}

	t.Run("an unreadable table is an error", func(t *testing.T) {
		t.Parallel()
		r := runner.ProcessRunner{Processes: unreadableTable{}}

		if _, err := r.Liveness(pid, now, now); !errors.Is(err, errTable) {
			t.Errorf("Liveness() error = %v, want %v", err, errTable)
		}
	})

	t.Run("ps reads a running process as alive", func(t *testing.T) {
		t.Parallel()
		pid, startedAt := startRealProcess(t)

		alive, err := (runner.ProcessRunner{}).Liveness(pid, startedAt, time.Now())
		if err != nil {
			t.Fatalf("Liveness() error = %v", err)
		}
		if !alive {
			t.Errorf("Liveness(%d) = false, want true for a running process", pid)
		}
	})

	t.Run("ps reads a reaped pid as dead", func(t *testing.T) {
		t.Parallel()
		cmd := exec.Command("/bin/sh", "-c", "exit 0")
		if err := cmd.Run(); err != nil {
			t.Fatalf("Run(exit 0): %v", err)
		}
		pid := cmd.Process.Pid

		alive, err := (runner.ProcessRunner{}).Liveness(pid, time.Now(), time.Now())
		if err != nil {
			t.Fatalf("Liveness() error = %v", err)
		}
		if alive {
			t.Errorf("Liveness(%d) = true, want false for a reaped child", pid)
		}
	})
}

type steppingClock struct{ now time.Time }

func (c *steppingClock) Now() time.Time { return c.now }

func (c *steppingClock) After(d time.Duration) <-chan time.Time {
	c.now = c.now.Add(d)
	fired := make(chan time.Time, 1)
	fired <- c.now
	return fired
}

func spawnReady(t *testing.T, r runner.ProcessRunner, body string) (pid int, readyLine string) {
	t.Helper()
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	t.Cleanup(func() {
		if err := read.Close(); err != nil {
			t.Errorf("close pipe: %v", err)
		}
	})
	cfg := runner.SpawnConfig{AgentCommand: []string{agentScript(t, body)}, WorktreePath: t.TempDir(), LogFile: write}
	result, err := r.Spawn(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if err := write.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}
	line, err := bufio.NewReader(read).ReadString('\n')
	if err != nil {
		t.Fatalf("read the agent's ready line: %v", err)
	}
	return result.Pid, strings.TrimSpace(line)
}

func running(t *testing.T, pid int) bool {
	t.Helper()
	out, err := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	stat := strings.TrimSpace(string(out))
	return err == nil && stat != "" && !strings.HasPrefix(stat, "Z")
}

func TestProcessRunner_Cancel(t *testing.T) {
	t.Parallel()

	t.Run("SIGTERM ends the leader and its child", func(t *testing.T) {
		t.Parallel()
		r := runner.ProcessRunner{}
		pgid, line := spawnReady(t, r, "sleep 300 &\necho $!\nwait\n")
		child, err := strconv.Atoi(line)
		if err != nil {
			t.Fatalf("parse child pid %q: %v", line, err)
		}

		if err := r.Cancel(pgid); err != nil {
			t.Fatalf("Cancel(%d) error = %v", pgid, err)
		}

		if code, ok := r.Reap(pgid); !ok || code != 128+int(syscall.SIGTERM) {
			t.Errorf("Reap(%d) after Cancel = %d, %v, want %d, true", pgid, code, ok, 128+int(syscall.SIGTERM))
		}
		if running(t, child) {
			t.Errorf("child %d still running after Cancel(%d)", child, pgid)
		}
	})

	t.Run("a group that ignores SIGTERM is killed once the deadline passes", func(t *testing.T) {
		t.Parallel()
		r := runner.ProcessRunner{Clock: &steppingClock{now: time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)}}
		pgid, _ := spawnReady(t, r, "trap '' TERM\necho ready\nwhile :; do sleep 1; done\n")

		if err := r.Cancel(pgid); err != nil {
			t.Fatalf("Cancel(%d) error = %v", pgid, err)
		}

		if code, ok := r.Reap(pgid); !ok || code != 128+int(syscall.SIGKILL) {
			t.Errorf("Reap(%d) after Cancel = %d, %v, want %d, true", pgid, code, ok, 128+int(syscall.SIGKILL))
		}
	})
}

func TestProcessRunner(t *testing.T) {
	t.Parallel()
	runnertest.RunContract(t, func(*testing.T) runner.Runner { return runner.ProcessRunner{} })
}

func TestProcessRunner_Reap(t *testing.T) {
	t.Parallel()

	t.Run("returns our child's exit code", func(t *testing.T) {
		t.Parallel()
		cmd := exec.Command("/bin/sh", "-c", "exit 3")
		if err := cmd.Start(); err != nil {
			t.Fatalf("Start(exit 3): %v", err)
		}

		code, ok := (runner.ProcessRunner{}).Reap(cmd.Process.Pid)
		if !ok || code != 3 {
			t.Errorf("Reap(%d) = %d, %v, want 3, true", cmd.Process.Pid, code, ok)
		}
	})

	t.Run("a pid that is not our child", func(t *testing.T) {
		t.Parallel()
		if _, ok := (runner.ProcessRunner{}).Reap(1); ok {
			t.Error("Reap(1) ok = true, want false")
		}
	})
}
