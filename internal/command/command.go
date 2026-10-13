package command

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	maxCommandLine = 200
	defaultTimeout = 2 * time.Minute
	waitDelay      = 2 * time.Second
)

func Output(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	return OutputWithin(ctx, defaultTimeout, dir, name, args...)
}

func OutputWithin(ctx context.Context, timeout time.Duration, dir, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd, stderr := build(ctx, dir, name, args)
	out, err := cmd.Output()
	return out, wrap(err, cmd, stderr)
}

func Run(ctx context.Context, dir, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()
	cmd, stderr := build(ctx, dir, name, args)
	return wrap(cmd.Run(), cmd, stderr)
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
		line = line[:maxCommandLine] + "..."
	}
	if cmd.Dir != "" {
		line += " in " + cmd.Dir
	}
	return fmt.Errorf("%s: %w: %s", line, err, bytes.TrimSpace(stderr.Bytes()))
}
