package command_test

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/command"
)

func TestOutputReturnsStdoutRunInDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	out, err := command.Output(t.Context(), dir, "pwd")
	if err != nil {
		t.Fatalf("Output(pwd): %v", err)
	}
	if got := strings.TrimSpace(string(out)); !strings.HasSuffix(got, dir) {
		t.Errorf("Output(pwd) = %q, want a path ending %q", got, dir)
	}
}

func TestOutputErrorNamesCommandDirAndStderr(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	longArg := strings.Repeat("x", 5000)
	line := `echo "$GIT_TERMINAL_PROMPT $GH_PROMPT_DISABLED"; echo partial; echo boom >&2; exit 3`

	out, err := command.Output(t.Context(), dir, "sh", "-c", line, longArg)
	if err == nil {
		t.Fatal("Output(exit 3) = nil error, want one")
	}
	var cmdErr *command.Error
	if !errors.As(err, &cmdErr) {
		t.Fatalf("Output error = %v, want *command.Error", err)
	}
	if !strings.HasPrefix(cmdErr.Cmd, "sh -c "+line) || len(cmdErr.Cmd) > 200 ||
		!strings.HasSuffix(cmdErr.Cmd, "...") || cmdErr.Dir != dir || cmdErr.Stderr != "boom" {
		t.Errorf("Output error fields = %+v, want truncated command, dir and stderr", cmdErr)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 3 {
		t.Errorf("Output error = %v, want an *exec.ExitError with code 3", err)
	}
	if strings.TrimSpace(string(out)) != "0 1\npartial" {
		t.Errorf("Output stdout on failure = %q, want prompts disabled and partial output", out)
	}
}

func TestRunDiscardsStdoutAndNamesTheFailure(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	err := command.Run(t.Context(), dir, "sh", "-c", "echo boom >&2; exit 2")
	if err == nil {
		t.Fatal("Run(exit 2) = nil error, want one")
	}
	var cmdErr *command.Error
	if !errors.As(err, &cmdErr) {
		t.Fatalf("Run error = %v, want *command.Error", err)
	}
	if cmdErr.Cmd != "sh -c echo boom >&2; exit 2" || cmdErr.Dir != dir || cmdErr.Stderr != "boom" {
		t.Errorf("Run error fields = %+v, want command, dir and stderr", cmdErr)
	}
}

func TestOutputReturnsWhenAGrandchildHoldsStdoutOpen(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := command.Output(ctx, "", "sh", "-c", "sleep 30 & wait")
	if err == nil {
		t.Fatal("Output(stalled child) = nil error, want one")
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("Output(stalled child) took %v, want it bounded by timeout plus WaitDelay", elapsed)
	}
}
