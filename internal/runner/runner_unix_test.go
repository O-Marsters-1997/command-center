//go:build unix

package runner_test

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/runner"
)

func startRealProcess(t *testing.T, seconds int) (pid int, startedAt time.Time) {
	t.Helper()
	cmd := exec.Command("sleep", strconv.Itoa(seconds))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start real process: %v", err)
	}
	startedAt = time.Now()
	t.Cleanup(func() {
		if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			t.Errorf("Kill(sleep): %v", err)
		}
		var exitErr *exec.ExitError
		if err := cmd.Wait(); err != nil && !errors.As(err, &exitErr) {
			t.Errorf("Wait(sleep): %v", err)
		}
	})
	return cmd.Process.Pid, startedAt
}

func TestLivenessReportsAliveForARunningProcessWithAMatchingStartTime(t *testing.T) {
	t.Parallel()
	pid, startedAt := startRealProcess(t, 10)

	alive, err := runner.ProcessRunner{}.Liveness(pid, startedAt, time.Now())
	if err != nil {
		t.Fatalf("Liveness: %v", err)
	}
	if !alive {
		t.Error("Liveness reported dead for a running process with a matching start time")
	}
}

func TestLivenessReportsDeadForAReapedPid(t *testing.T) {
	t.Parallel()

	// macOS ps rejects oversized pid values, so use a child pid that the test has reaped.
	cmd := exec.Command("/bin/sh", "-c", "exit 0")
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start(reaped child): %v", err)
	}
	pid := cmd.Process.Pid
	if err := cmd.Wait(); err != nil {
		t.Fatalf("Wait(reaped child): %v", err)
	}
	alive, err := runner.ProcessRunner{}.Liveness(pid, time.Now(), time.Now())
	if err != nil {
		t.Fatalf("Liveness: %v", err)
	}
	if alive {
		t.Error("Liveness reported alive for a reaped child")
	}
}

func TestLivenessReportsDeadForALivePidRecordedWithADifferentStartTime(t *testing.T) {
	t.Parallel()
	pid, startedAt := startRealProcess(t, 30)
	reusedPidStart := startedAt.Add(6 * time.Second)

	now := time.Now()
	aliveSelf, err := runner.ProcessRunner{}.Liveness(pid, startedAt, now)
	if err != nil {
		t.Fatalf("Liveness(pid, startedAt): %v", err)
	}
	if !aliveSelf {
		t.Fatal("Liveness reported the process dead against its own real start time")
	}

	aliveMismatched, err := runner.ProcessRunner{}.Liveness(pid, reusedPidStart, now)
	if err != nil {
		t.Fatalf("Liveness(pid, reusedPidStart): %v", err)
	}
	if aliveMismatched {
		t.Error("Liveness matched the pid against a start time 6s off its real start; " +
			"the anti-pid-reuse tolerance should have caught this")
	}
}

func TestReapReturnsTheRealExitCode(t *testing.T) {
	t.Parallel()
	cmd := exec.Command("/bin/sh", "-c", "exit 3")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start(exit 3): %v", err)
	}
	pid := cmd.Process.Pid

	dead := func() bool {
		alive, err := runner.ProcessRunner{}.Liveness(pid, time.Now(), time.Now())
		if err != nil {
			t.Fatalf("Liveness: %v", err)
		}
		return !alive
	}
	if !pollUntil(2*time.Second, dead) {
		t.Fatal("process never went dead")
	}

	exitCode, ok := runner.ProcessRunner{}.Reap(pid)
	if !ok {
		t.Fatal("Reap reported ok=false for our own direct child")
	}
	if exitCode != 3 {
		t.Errorf("exit code = %d, want 3", exitCode)
	}
}

func TestReapReportsNotOkForAPidThatIsNotOurChild(t *testing.T) {
	t.Parallel()

	if _, ok := (runner.ProcessRunner{}).Reap(1); ok {
		t.Error("Reap reported ok=true for pid 1, which is never our child")
	}
}

func TestLivenessReportsDeadForAnExitedProcess(t *testing.T) {
	t.Parallel()
	cmd := exec.Command("/bin/sh", "-c", "exit 0")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start(exit 0): %v", err)
	}
	pid := cmd.Process.Pid
	startedAt := time.Now()

	dead := func() bool {
		alive, err := runner.ProcessRunner{}.Liveness(pid, startedAt, time.Now())
		if err != nil {
			t.Fatalf("Liveness: %v", err)
		}
		return !alive
	}
	if !pollUntil(2*time.Second, dead) {
		t.Error("Liveness still reports alive for a process that exited immediately")
	}
	if _, ok := (runner.ProcessRunner{}).Reap(pid); !ok {
		t.Errorf("Reap(%d) = not a child, want child", pid)
	}
}
