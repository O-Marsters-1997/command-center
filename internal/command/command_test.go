package command_test

import (
	"errors"
	"os/exec"
	"strings"
	"testing"

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
	for _, want := range []string{"sh -c echo partial; echo boom >&2; exit 3", "in " + dir, "boom"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Output error = %q, want it to contain %q", err, want)
		}
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
	for _, want := range []string{"sh -c", "in " + dir, "boom"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Run error = %q, want it to contain %q", err, want)
		}
	}
}

func TestOutputErrorTruncatesALongCommandLine(t *testing.T) {
	body := strings.Repeat("x", 5000)

	_, err := command.Output(t.Context(), "", "sh", "-c", "exit 1", body)
	if err == nil {
		t.Fatal("Output(exit 1) = nil error, want one")
	}
	if strings.Contains(err.Error(), body) || len(err.Error()) > 300 {
		t.Errorf("Output error has %d bytes, want the command line truncated", len(err.Error()))
	}
	if strings.Contains(err.Error(), " in ") {
		t.Errorf("Output error = %q, want no dir for an empty dir", err)
	}
}
