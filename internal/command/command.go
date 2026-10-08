package command

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

const maxCommandLine = 200

func Output(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	cmd, stderr := build(ctx, dir, name, args)
	out, err := cmd.Output()
	return out, wrap(err, cmd, stderr)
}

func Run(ctx context.Context, dir, name string, args ...string) error {
	cmd, stderr := build(ctx, dir, name, args)
	return wrap(cmd.Run(), cmd, stderr)
}

func build(ctx context.Context, dir, name string, args []string) (*exec.Cmd, *bytes.Buffer) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
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
