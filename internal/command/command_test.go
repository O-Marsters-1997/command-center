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
	dir := t.TempDir()

	out, err := command.Output(t.Context(), dir, "sh", "-c", "echo partial; echo boom >&2; exit 3")
	if err == nil {
		t.Fatal("Output(exit 3) = nil error, want one")
	}
	var cmdErr *command.Error
	if !errors.As(err, &cmdErr) {
		t.Fatalf("Output error = %v, want *command.Error", err)
	}
	if cmdErr.Cmd != "sh -c echo partial; echo boom >&2; exit 3" || cmdErr.Dir != dir || cmdErr.Stderr != "boom" {
		t.Errorf("Output error fields = %+v, want command, dir and stderr", cmdErr)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 3 {
		t.Errorf("Output error = %v, want an *exec.ExitError with code 3", err)
	}
	if strings.TrimSpace(string(out)) != "partial" {
		t.Errorf("Output stdout on failure = %q, want %q", out, "partial")
	}
}

func TestRunDiscardsStdoutAndNamesTheFailure(t *testing.T) {
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

func TestOutputSwitchesPromptsOff(t *testing.T) {
	out, err := command.Output(t.Context(), "", "sh", "-c", `echo "$GIT_TERMINAL_PROMPT $GH_PROMPT_DISABLED"`)
	if err != nil {
		t.Fatalf("Output(echo env): %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != "0 1" {
		t.Errorf("GIT_TERMINAL_PROMPT and GH_PROMPT_DISABLED = %q, want %q", got, "0 1")
	}
}

func TestOutputErrorTruncatesALongCommandLine(t *testing.T) {
	body := strings.Repeat("x", 5000)

	_, err := command.Output(t.Context(), "", "sh", "-c", "exit 1", body)
	if err == nil {
		t.Fatal("Output(exit 1) = nil error, want one")
	}
	var cmdErr *command.Error
	if !errors.As(err, &cmdErr) {
		t.Fatalf("Output error = %v, want *command.Error", err)
	}
	if len(cmdErr.Cmd) > 200 || cmdErr.Dir != "" {
		t.Errorf("Output command = %q, dir = %q, want at most 200 bytes and no dir", cmdErr.Cmd, cmdErr.Dir)
	}
}
