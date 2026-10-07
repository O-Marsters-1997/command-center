//go:build unix

package runner

import (
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const pidReuseTolerance = 5 * time.Second

// Liveness reports whether pgid is still the process launched at wantStart. It uses ps, not
// kill(-pgid, 0): on Darwin a group whose leader is a zombie returns EPERM, not ESRCH.
func Liveness(pgid int, wantStart, now time.Time) (bool, error) {
	stat, etime, ok := psStatAndEtime(pgid)
	if !ok {
		return false, nil
	}
	if strings.HasPrefix(stat, "Z") {
		return false, nil
	}

	elapsed, err := parseEtime(etime)
	if err != nil {
		return false, fmt.Errorf("parse ps etime %q for pid %d: %w", etime, pgid, err)
	}

	gotStart := now.Add(-elapsed)
	drift := gotStart.Sub(wantStart)
	if drift < 0 {
		drift = -drift
	}
	return drift <= pidReuseTolerance, nil
}

func psStatAndEtime(pid int) (stat, etime string, ok bool) {
	out, err := exec.Command("ps", "-o", "stat=,etime=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return "", "", false
	}
	fields := strings.Fields(string(out))
	if len(fields) != 2 {
		return "", "", false
	}
	return fields[0], fields[1], true
}

// ps's elapsed-time format is [[DD-]hh:]mm:ss on both GNU and BSD.
func parseEtime(s string) (time.Duration, error) {
	days := 0
	if before, after, found := strings.Cut(s, "-"); found {
		d, err := strconv.Atoi(before)
		if err != nil {
			return 0, fmt.Errorf("day component %q: %w", before, err)
		}
		days, s = d, after
	}

	parts := strings.Split(s, ":")
	var h, m, sec int
	var err error
	switch len(parts) {
	case 2:
		if m, err = strconv.Atoi(parts[0]); err != nil {
			return 0, fmt.Errorf("minutes %q: %w", parts[0], err)
		}
		if sec, err = strconv.Atoi(parts[1]); err != nil {
			return 0, fmt.Errorf("seconds %q: %w", parts[1], err)
		}
	case 3:
		if h, err = strconv.Atoi(parts[0]); err != nil {
			return 0, fmt.Errorf("hours %q: %w", parts[0], err)
		}
		if m, err = strconv.Atoi(parts[1]); err != nil {
			return 0, fmt.Errorf("minutes %q: %w", parts[1], err)
		}
		if sec, err = strconv.Atoi(parts[2]); err != nil {
			return 0, fmt.Errorf("seconds %q: %w", parts[2], err)
		}
	default:
		return 0, fmt.Errorf("unrecognised format %q", s)
	}

	total := time.Duration(days)*24*time.Hour + time.Duration(h)*time.Hour +
		time.Duration(m)*time.Minute + time.Duration(sec)*time.Second
	return total, nil
}

const (
	cancelPollInterval = 20 * time.Millisecond
	cancelDeadline     = 2 * time.Second
)

// Cancel terminates every process in pgid: SIGTERM, then poll, then SIGKILL as a backstop.
func Cancel(pgid int) error {
	if err := syscall.Kill(-pgid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("SIGTERM process group %d: %w", pgid, err)
	}

	deadline := time.Now().Add(cancelDeadline)
	for time.Now().Before(deadline) {
		// Darwin returns EPERM, not ESRCH, once the group's last member is a zombie.
		if err := syscall.Kill(-pgid, 0); err != nil {
			return nil
		}
		time.Sleep(cancelPollInterval)
	}

	if err := syscall.Kill(-pgid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("SIGKILL process group %d: %w", pgid, err)
	}
	return nil
}

// Reap collects a dead child's exit code. ok is false when pid is not our direct child, as after
// a restart when a recovered run belongs to init.
func Reap(pid int) (exitCode int, ok bool) {
	var status syscall.WaitStatus
	if _, err := syscall.Wait4(pid, &status, 0, nil); err != nil {
		return 0, false
	}
	switch {
	case status.Exited():
		return status.ExitStatus(), true
	case status.Signaled():
		return 128 + int(status.Signal()), true
	default:
		return 0, true
	}
}

func (ProcessRunner) Liveness(pgid int, wantStart, now time.Time) (bool, error) {
	return Liveness(pgid, wantStart, now)
}

func (ProcessRunner) Cancel(pgid int) error { return Cancel(pgid) }

func (ProcessRunner) Reap(pid int) (int, bool) { return Reap(pid) }
