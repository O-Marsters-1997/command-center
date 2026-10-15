package command

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	maxCommandLine = 200
	DefaultTimeout = 2 * time.Minute
	waitDelay      = 2 * time.Second
)

type Executor interface {
	Output(ctx context.Context, timeout time.Duration, dir, name string, args ...string) ([]byte, error)
}

type Exec struct{}

func (Exec) Output(ctx context.Context, timeout time.Duration, dir, name string, args ...string) ([]byte, error) {
	return OutputWithin(ctx, timeout, dir, name, args...)
}

type ExecutorFunc func(ctx context.Context, timeout time.Duration, dir, name string, args ...string) ([]byte, error)

func (f ExecutorFunc) Output(
	ctx context.Context, timeout time.Duration, dir, name string, args ...string,
) ([]byte, error) {
	return f(ctx, timeout, dir, name, args...)
}

func OrExec(e Executor) Executor {
	if e == nil {
		return Exec{}
	}
	return e
}

// Error carries the command, working directory and stderr of a failed process.
type Error struct {
	Cmd, Dir, Stderr string
	Err              error
}

func (e *Error) Error() string {
	line := e.Cmd
	if e.Dir != "" {
		line += " in " + e.Dir
	}
	return fmt.Sprintf("%s: %v: %s", line, e.Err, e.Stderr)
}

func (e *Error) Unwrap() error { return e.Err }

func Output(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	return OutputWithin(ctx, DefaultTimeout, dir, name, args...)
}

func OutputWithin(ctx context.Context, timeout time.Duration, dir, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd, stderr := build(ctx, dir, name, args)
	out, err := cmd.Output()
	return out, finish(ctx, timeout, err, cmd, stderr)
}

func Run(ctx context.Context, dir, name string, args ...string) error {
	return RunWithin(ctx, DefaultTimeout, dir, name, args...)
}

func RunWithin(ctx context.Context, timeout time.Duration, dir, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd, stderr := build(ctx, dir, name, args)
	return finish(ctx, timeout, cmd.Run(), cmd, stderr)
}

func finish(ctx context.Context, timeout time.Duration, err error, cmd *exec.Cmd, stderr *bytes.Buffer) error {
	err = wrap(err, cmd, stderr)
	if err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("timed out after %s: %w", timeout, err)
	}
	return err
}

func build(ctx context.Context, dir, name string, args []string) (*exec.Cmd, *bytes.Buffer) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.WaitDelay = waitDelay
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GH_PROMPT_DISABLED=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	return cmd, &stderr
}

func wrap(err error, cmd *exec.Cmd, stderr *bytes.Buffer) error {
	if err == nil {
		return nil
	}
	line := strings.Join(cmd.Args, " ")
	if len(line) > maxCommandLine {
		line = line[:maxCommandLine-3] + "..."
	}
	return &Error{Cmd: line, Dir: cmd.Dir, Stderr: string(bytes.TrimSpace(stderr.Bytes())), Err: err}
}
