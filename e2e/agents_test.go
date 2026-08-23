// Package e2e_test exercises the fake agent scripts under testdata/agents against a real git
// repository. Their argv contract is: $1 worktree dir, $2 settings path, $3 prompt file.
package e2e_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func newWorktree(t *testing.T) (dir, settings, prompt string) {
	t.Helper()
	dir = t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	git(t, dir, "config", "user.name", "Test Agent")
	git(t, dir, "config", "user.email", "agent@example.com")
	write(t, filepath.Join(dir, "README.md"), "worktree\n")
	git(t, dir, "add", "README.md")
	git(t, dir, "commit", "-q", "-m", "initial commit")

	settings = filepath.Join(t.TempDir(), "settings.json")
	write(t, settings, "{}\n")
	prompt = filepath.Join(t.TempDir(), "prompt.md")
	write(t, prompt, "do the thing\n")
	return dir, settings, prompt
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

func agentScript(t *testing.T, name string) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "testdata", "agents", name))
	if err != nil {
		t.Fatalf("resolving %s: %v", name, err)
	}
	return path
}

func commitCount(t *testing.T, dir string) string {
	t.Helper()
	return git(t, dir, "rev-list", "--count", "HEAD")
}

func TestAgentScripts(t *testing.T) {
	t.Run("commits", func(t *testing.T) {
		t.Parallel()

		dir, settings, prompt := newWorktree(t)
		before := commitCount(t, dir)

		out, err := exec.Command(agentScript(t, "commits.sh"), dir, settings, prompt).CombinedOutput()
		if err != nil {
			t.Fatalf("commits.sh: %v\n%s", err, out)
		}

		after := commitCount(t, dir)
		if before != "1" || after != "2" {
			t.Errorf("commit count went %s -> %s, want 1 -> 2", before, after)
		}
		if status := git(t, dir, "status", "--porcelain"); status != "" {
			t.Errorf("status = %q, want a clean worktree", status)
		}
	})

	t.Run("empty", func(t *testing.T) {
		t.Parallel()

		dir, settings, prompt := newWorktree(t)
		before := git(t, dir, "rev-parse", "HEAD")

		out, err := exec.Command(agentScript(t, "empty.sh"), dir, settings, prompt).CombinedOutput()
		if err != nil {
			t.Fatalf("empty.sh: %v\n%s", err, out)
		}

		if after := git(t, dir, "rev-parse", "HEAD"); after != before {
			t.Errorf("HEAD moved %s -> %s, want it unchanged", before, after)
		}
		if status := git(t, dir, "status", "--porcelain"); status != "" {
			t.Errorf("status = %q, want nothing committed and nothing left behind", status)
		}
	})

	t.Run("sleeps", func(t *testing.T) {
		t.Parallel()

		dir, settings, prompt := newWorktree(t)

		cmd := exec.Command(agentScript(t, "sleeps.sh"), dir, settings, prompt)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			t.Fatalf("starting sleeps.sh: %v", err)
		}
		pgid := cmd.Process.Pid
		t.Cleanup(func() { _ = syscall.Kill(-pgid, syscall.SIGKILL) })

		waitForFile(t, filepath.Join(dir, "ready"))
		child := readPID(t, filepath.Join(dir, "child.pid"))
		if child == pgid {
			t.Fatalf("child pid %d equals the script pid, want a separate process", child)
		}

		if err := syscall.Kill(-pgid, syscall.SIGTERM); err != nil {
			t.Fatalf("killing process group %d: %v", pgid, err)
		}

		if err := cmd.Wait(); err == nil {
			t.Error("sleeps.sh exited 0, want it terminated by the signal")
		}
		waitForExit(t, child)
	})
}

// waitForFile polls for path, which the script touches once it is fully set up. Polling rather
// than sleeping keeps the test deterministic on a loaded machine.
func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}

func waitForExit(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		// Signal 0 probes for existence; ESRCH means the process is gone.
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("process %d still alive after the group was signalled", pid)
}

func readPID(t *testing.T, path string) int {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("parsing %s (%q): %v", path, raw, err)
	}
	return pid
}
