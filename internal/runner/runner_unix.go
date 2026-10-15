//go:build unix

package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/command"
)

const (
	pidReuseTolerance = 5 * time.Second
	psTimeout         = 10 * time.Second
)

// Spawn starts an agent as a process-group leader.
// Go's exec.CommandContext kills only its leader on cancellation, not its subprocesses.
func (ProcessRunner) Spawn(_ context.Context, cfg SpawnConfig) (SpawnResult, error) {
	if len(cfg.AgentCommand) == 0 {
		return SpawnResult{}, errors.New("spawn agent: agent_command is empty")
	}
	argv := buildArgv(cfg)
	if len(argv) == 0 {
		return SpawnResult{}, errors.New("spawn agent: agent_command builds an empty argv")
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = cfg.WorktreePath
	cmd.Stdout = cfg.LogFile
	cmd.Stderr = cfg.LogFile
	cmd.Env = stripAPIKey(os.Environ())
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return SpawnResult{}, fmt.Errorf("spawn agent %s: %w", argv[0], err)
	}
	return SpawnResult{Pid: cmd.Process.Pid}, nil
}

// Liveness reports whether pgid is still the process launched at wantStart. It uses ps, not
// kill(-pgid, 0): on Darwin a group whose leader is a zombie returns EPERM, not ESRCH.
func (r ProcessRunner) Liveness(pgid int, wantStart, now time.Time) (bool, error) {
	p, found, err := r.processes().Lookup(pgid)
	if err != nil {
		return false, fmt.Errorf("look up pid %d: %w", pgid, err)
	}
	if !found || p.Zombie {
		return false, nil
	}

	gotStart := now.Add(-p.Elapsed)
	drift := gotStart.Sub(wantStart)
	if drift < 0 {
		drift = -drift
	}
	return drift <= pidReuseTolerance, nil
}

type PS struct{}

func (PS) Lookup(pid int) (Process, bool, error) {
	out, err := command.OutputWithin(
		context.Background(), psTimeout, "", "ps", "-o", "stat=,etime=", "-p", strconv.Itoa(pid),
	)
	if err != nil {
		return Process{}, false, nil
	}
	fields := strings.Fields(string(out))
	if len(fields) != 2 {
		return Process{}, false, nil
	}
	stat, etime := fields[0], fields[1]
	if strings.HasPrefix(stat, "Z") {
		return Process{Zombie: true}, true, nil
	}
	elapsed, err := parseEtime(etime)
	if err != nil {
		return Process{}, false, fmt.Errorf("parse ps etime %q: %w", etime, err)
	}
	return Process{Elapsed: elapsed}, true, nil
}

type wallClock struct{}

func (wallClock) Now() time.Time                         { return time.Now() }
func (wallClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

func (r ProcessRunner) clock() Clock {
	if r.Clock == nil {
		return wallClock{}
	}
	return r.Clock
}

func (r ProcessRunner) processes() ProcessTable {
	if r.Processes == nil {
		return PS{}
	}
	return r.Processes
}

// GNU and BSD ps both emit [[DD-]hh:]mm:ss for elapsed time.
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

func (r ProcessRunner) Cancel(pgid int) error {
	if err := syscall.Kill(-pgid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("SIGTERM process group %d: %w", pgid, err)
	}

	clock := r.clock()
	deadline := clock.Now().Add(cancelDeadline)
	for clock.Now().Before(deadline) {
		// Darwin returns EPERM, not ESRCH, once the group's last member is a zombie.
		if err := syscall.Kill(-pgid, 0); err != nil {
			return nil
		}
		<-clock.After(cancelPollInterval)
	}

	if err := syscall.Kill(-pgid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("SIGKILL process group %d: %w", pgid, err)
	}
	return nil
}

func (ProcessRunner) Reap(pid int) (exitCode int, ok bool) {
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
