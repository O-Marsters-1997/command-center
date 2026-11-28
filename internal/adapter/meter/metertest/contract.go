//go:build unix

package metertest

import (
	"os/exec"
	"strings"
	"syscall"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/adapter/meter"
)

func RunContract(t *testing.T, newMeter func(t *testing.T, world []meter.Process) meter.Meter) {
	t.Helper()

	spawn := func(t *testing.T) meter.Process {
		t.Helper()
		cmd := exec.Command("sleep", "30")
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			t.Fatalf("start sleep: %v", err)
		}
		t.Cleanup(func() {
			if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
				t.Errorf("kill process group %d: %v", cmd.Process.Pid, err)
			}
			_ = cmd.Wait()
		})
		pid := cmd.Process.Pid
		return meter.Process{Pgid: pid, Pid: pid, Name: "sleep", Args: "sleep 30", RSS: 1 << 20}
	}

	t.Run("reads the given group and ignores others", func(t *testing.T) {
		t.Parallel()
		mine, other := spawn(t), spawn(t)
		m := newMeter(t, []meter.Process{mine, other})

		got, err := m.Read(t.Context(), []int{mine.Pgid})
		if err != nil {
			t.Fatalf("Read(%d) error = %v", mine.Pgid, err)
		}
		if len(got.Procs) != 1 {
			t.Fatalf("Read(%d) procs = %+v, want exactly the one in the group", mine.Pgid, got.Procs)
		}
		p := got.Procs[0]
		if p.Pgid != mine.Pgid || p.Pid != mine.Pid || p.Name != "sleep" ||
			!strings.HasPrefix(p.Args, "sleep") || p.RSS <= 0 {
			t.Errorf("Read(%d) proc = %+v, want sleep pid %d with args and RSS > 0", mine.Pgid, p, mine.Pid)
		}
	})

	t.Run("no pgids still reads the machine", func(t *testing.T) {
		t.Parallel()
		got, err := newMeter(t, nil).Read(t.Context(), nil)
		if err != nil {
			t.Fatalf("Read(nil) error = %v", err)
		}
		if got.Total <= 0 || got.Used <= 0 || got.Used > got.Total {
			t.Errorf("Read(nil) total, used = %d, %d, want 0 < used <= total", got.Total, got.Used)
		}
		if len(got.Procs) != 0 {
			t.Errorf("Read(nil) procs = %+v, want none", got.Procs)
		}
	})
}
