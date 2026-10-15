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

func sleepsScript(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs("testdata/sleeps.sh")
	if err != nil {
		t.Fatalf("Abs(sleeps.sh): %v", err)
	}
	return abs
}

func TestCancelTerminatesTheLeaderAndItsChild(t *testing.T) {
	t.Parallel()
	worktree := t.TempDir()
	logFile, err := os.Create(filepath.Join(t.TempDir(), "run.jsonl"))
	if err != nil {
		t.Fatalf("Create(run log): %v", err)
	}
	t.Cleanup(func() { _ = logFile.Close() })

	cfg := runner.SpawnConfig{
		AgentCommand: []string{sleepsScript(t), "{worktree}", "{settings}", "{prompt_file}"},
		WorktreePath: worktree,
		SettingsPath: filepath.Join(t.TempDir(), "agent.json"),
		PromptPath:   filepath.Join(t.TempDir(), "prompt.txt"),
		LogFile:      logFile,
	}
	result, err := (runner.ProcessRunner{}).Spawn(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	pgid := result.Pid

	waitForFile(t, filepath.Join(worktree, "ready"))
	childPid := readChildPid(t, filepath.Join(worktree, "child.pid"))

	alive, err := runner.ProcessRunner{}.Liveness(pgid, time.Now(), time.Now())
	if err != nil {
		t.Fatalf("Liveness before cancel: %v", err)
	}
	if !alive {
		t.Fatal("sleeps.sh reads dead before Cancel was even called")
	}

	if err := (runner.ProcessRunner{}).Cancel(pgid); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	if aliveAfter, err := (runner.ProcessRunner{}).Liveness(pgid, time.Now(), time.Now()); err != nil || aliveAfter {
		t.Errorf("leader still reads alive=%v (err=%v) after Cancel", aliveAfter, err)
	}
	if !eventuallyNotRunning(childPid) {
		t.Error("the child sleep process spawned by sleeps.sh survived Cancel")
	}
}

func readChildPid(t *testing.T, path string) int {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read child pid file: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("parse child pid %q: %v", raw, err)
	}
	return pid
}

func eventuallyNotRunning(pid int) bool {
	return pollUntil(2*time.Second, func() bool {
		out, err := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
		stat := strings.TrimSpace(string(out))
		return err != nil || stat == "" || strings.HasPrefix(stat, "Z")
	})
}
