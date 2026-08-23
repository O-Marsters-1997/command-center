//go:build unix

package cc

import (
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// pidReuseTolerance bounds how far a process's real start time may drift from the recorded one
// and still count as the same process. It exists because a bare pid can be recycled by the OS;
// judged negligible against the loop's 15s tick period (a recycled pid within this window reads
// as the same process — a known, accepted gap, not a closed one).
const pidReuseTolerance = 5 * time.Second

// Liveness reports whether the process recorded as pgid is still the one launched at wantStart.
// It shells out to `ps -o stat=,etime=` rather than kill(-pgid, 0): on Darwin, signalling a
// process group whose leader has become a zombie returns EPERM, not ESRCH, so a kill-based check
// would read a just-finished run as alive forever.
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

// psStatAndEtime runs `ps -o stat=,etime= -p pid` and reports its two fields. ok is false for
// any failure — no matching process, a pid ps refuses outright, or unexpected output — all of
// which this package treats identically: nothing to report as alive.
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

// parseEtime parses ps's elapsed-time format, [[DD-]hh:]mm:ss, shared by GNU and BSD ps alike.
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

// cancelPollInterval and cancelDeadline bound Cancel's escalation: it blocks the loop goroutine
// for up to cancelDeadline rather than adding a kill-escalation goroutine, which the design
// forbids (§3).
const (
	cancelPollInterval = 20 * time.Millisecond
	cancelDeadline     = 2 * time.Second
)

// Cancel terminates every process in pgid: SIGTERM, then poll for it to disappear, then SIGKILL
// as a backstop. It signals -pgid (the whole process group), not just the leader's pid — unlike
// exec.Cmd's built-in Cancel/WaitDelay, which would leave a subprocess the agent spawned under the same pgid holding the worktree.
func Cancel(pgid int) error {
	if err := syscall.Kill(-pgid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("SIGTERM process group %d: %w", pgid, err)
	}

	deadline := time.Now().Add(cancelDeadline)
	for time.Now().Before(deadline) {
		// Any error here — including EPERM, which Darwin returns once the group's last
		// member is a zombie rather than ESRCH (see Liveness) — means there is nothing left
		// in this group willing to receive a signal from us, i.e. it is as good as gone.
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

// Reap collects a dead child's exit code, called once liveness has found a run dead. We never
// call cmd.Wait() at spawn time (that would need a goroutine per run, which the design forbids).
// ok is false when pid is not our direct child: after a restart, a recovered run belongs to init.
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

// Liveness, Cancel and Reap on ProcessRunner delegate to the free functions above, which is the
// shape lock.go already uses (Lock/Close are free functions too, not methods on a stateless
// type).
func (ProcessRunner) Liveness(pgid int, wantStart, now time.Time) (bool, error) {
	return Liveness(pgid, wantStart, now)
}

func (ProcessRunner) Cancel(pgid int) error { return Cancel(pgid) }

func (ProcessRunner) Reap(pid int) (int, bool) { return Reap(pid) }
