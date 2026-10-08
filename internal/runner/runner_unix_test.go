package runner_test

import (
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/runner"
)

// startRealProcess starts a real, briefly-long-lived process as its own group leader and
// returns its pid plus the wall-clock time it actually started. t.Cleanup kills it.
func startRealProcess(t *testing.T, seconds int) (pid int, startedAt time.Time) {
	t.Helper()
	cmd := exec.Command("sleep", itoaHelper(seconds))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start real process: %v", err)
	}
	startedAt = time.Now()
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	return cmd.Process.Pid, startedAt
}

func itoaHelper(n int) string {
	digits := "0123456789"
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{digits[n%10]}, b...)
		n /= 10
	}
	return string(b)
}

func TestLivenessReportsAliveForARunningProcessWithAMatchingStartTime(t *testing.T) {
	pid, startedAt := startRealProcess(t, 10)

	alive, err := runner.ProcessRunner{}.Liveness(pid, startedAt, time.Now())
	if err != nil {
		t.Fatalf("Liveness: %v", err)
	}
	if !alive {
		t.Error("Liveness reported dead for a running process with a matching start time")
	}
}

func TestLivenessReportsDeadForANonexistentPid(t *testing.T) {
	t.Parallel()

	// A pid that (almost certainly) names nothing at all. Deliberately 5 digits: macOS's ps
	// rejects some larger values outright ("process id too large") rather than reporting no
	// match, which would otherwise make this assertion fragile.
	alive, err := runner.ProcessRunner{}.Liveness(99999, time.Now(), time.Now())
	if err != nil {
		t.Fatalf("Liveness: %v", err)
	}
	if alive {
		t.Error("Liveness reported alive for a pid that does not exist")
	}
}

func TestLivenessReportsDeadForALivePidRecordedWithADifferentStartTime(t *testing.T) {
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
	cmd := exec.Command("/bin/sh", "-c", "exit 3")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
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
	cmd := exec.Command("/bin/sh", "-c", "exit 0")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
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
}
