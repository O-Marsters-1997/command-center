package command_test

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/command"
)

func TestOutput(t *testing.T) {
	t.Parallel()

	t.Run("returns stdout run in dir", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()

		out, err := command.Output(t.Context(), dir, "pwd")
		if err != nil {
			t.Fatalf("Output(pwd) error = %v", err)
		}
		if got := strings.TrimSpace(string(out)); !strings.HasSuffix(got, dir) {
			t.Errorf("Output(pwd) = %q, want a path ending %q", got, dir)
		}
	})

	t.Run("a failure carries command, dir, stderr and exit code", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()

		out, err := command.Output(t.Context(), dir, "sh", "-c", "echo partial; echo boom >&2; exit 3")
		cmdErr, ok := errors.AsType[*command.Error](err)
		if !ok {
			t.Fatalf("Output(exit 3) error = %v, want *command.Error", err)
		}
		want := command.Error{Cmd: "sh -c echo partial; echo boom >&2; exit 3", Dir: dir, Stderr: "boom"}
		got := command.Error{Cmd: cmdErr.Cmd, Dir: cmdErr.Dir, Stderr: cmdErr.Stderr}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Output(exit 3) error mismatch (-want +got):\n%s", diff)
		}
		if exitErr, ok := errors.AsType[*exec.ExitError](err); !ok || exitErr.ExitCode() != 3 {
			t.Errorf("Output(exit 3) error = %v, want an *exec.ExitError with code 3", err)
		}
		if got := strings.TrimSpace(string(out)); got != "partial" {
			t.Errorf("Output(exit 3) stdout = %q, want %q", got, "partial")
		}
	})

	t.Run("returns when a grandchild holds stdout open past the timeout", func(t *testing.T) {
		t.Parallel()

		if _, err := command.OutputWithin(t.Context(), time.Millisecond, "", "sh", "-c", "sleep 30 & wait"); err == nil {
			t.Fatal("OutputWithin(stalled child) error = nil, want a timeout")
		}
	})

	t.Run("switches prompts off", func(t *testing.T) {
		t.Parallel()

		out, err := command.Output(t.Context(), "", "sh", "-c", `echo "$GIT_TERMINAL_PROMPT $GH_PROMPT_DISABLED"`)
		if err != nil {
			t.Fatalf("Output(echo env) error = %v", err)
		}
		if got := strings.TrimSpace(string(out)); got != "0 1" {
			t.Errorf("Output(echo env) = %q, want %q", got, "0 1")
		}
	})

	t.Run("a long command line is truncated in the error", func(t *testing.T) {
		t.Parallel()
		body := strings.Repeat("x", 5000)

		_, err := command.Output(t.Context(), "", "sh", "-c", "exit 1", body)
		cmdErr, ok := errors.AsType[*command.Error](err)
		if !ok {
			t.Fatalf("Output(exit 1) error = %v, want *command.Error", err)
		}
		if len(cmdErr.Cmd) > 200 || cmdErr.Dir != "" {
			t.Errorf("Output(exit 1) error command = %q, dir = %q, want at most 200 bytes and no dir", cmdErr.Cmd, cmdErr.Dir)
		}
	})
}

func TestRun(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	err := command.Run(t.Context(), dir, "sh", "-c", "echo boom >&2; exit 2")
	cmdErr, ok := errors.AsType[*command.Error](err)
	if !ok {
		t.Fatalf("Run(exit 2) error = %v, want *command.Error", err)
	}
	want := command.Error{Cmd: "sh -c echo boom >&2; exit 2", Dir: dir, Stderr: "boom"}
	got := command.Error{Cmd: cmdErr.Cmd, Dir: cmdErr.Dir, Stderr: cmdErr.Stderr}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Run(exit 2) error mismatch (-want +got):\n%s", diff)
	}
}
