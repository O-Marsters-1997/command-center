package cc_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
)

func sleepsScript(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs("../../testdata/agents/sleeps.sh")
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// TestCancelTerminatesTheLeaderAndItsChild spawns the real sleeps.sh fixture, whose whole
// purpose (per its own comment) is to background a child sleep in the same pgid so Cancel's
// -pgid targeting is genuinely exercised rather than incidental.
func TestCancelTerminatesTheLeaderAndItsChild(t *testing.T) {
	worktree := t.TempDir()
	logFile, err := os.Create(filepath.Join(t.TempDir(), "run.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logFile.Close() })

	cfg := cc.SpawnConfig{
		AgentCommand: []string{sleepsScript(t), "{worktree}", "{settings}", "{prompt_file}"},
		WorktreePath: worktree,
		SettingsPath: filepath.Join(t.TempDir(), "agent.json"),
		PromptPath:   filepath.Join(t.TempDir(), "prompt.txt"),
		LogFile:      logFile,
	}
	result, err := (cc.ProcessRunner{}).Spawn(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	pgid := result.Pid

	waitForFile(t, filepath.Join(worktree, "ready"))
	childPid := readChildPid(t, filepath.Join(worktree, "child.pid"))

	alive, err := cc.Liveness(pgid, time.Now(), time.Now())
	if err != nil {
		t.Fatalf("Liveness before cancel: %v", err)
	}
	if !alive {
		t.Fatal("sleeps.sh reads dead before Cancel was even called")
	}

	if err := cc.Cancel(pgid); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	if aliveAfter, err := cc.Liveness(pgid, time.Now(), time.Now()); err != nil || aliveAfter {
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

// eventuallyNotRunning polls a plain pid (the child sleep is not itself a group leader — only
// sleeps.sh, which Cancel targets via -pgid, is) until ps reports it gone or a zombie. The
// child is reparented on the leader's death and reaped by init, not by us, so a brief zombie
// window right after Cancel returns is expected rather than a failure.
func eventuallyNotRunning(pid int) bool {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		out, err := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
		stat := strings.TrimSpace(string(out))
		if err != nil || stat == "" || strings.HasPrefix(stat, "Z") {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}
