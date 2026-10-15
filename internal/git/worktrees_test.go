package git_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/command"
	"github.com/O-Marsters-1997/command-center/internal/git"
)

func fakeTp(t *testing.T, exitCode int) (bin, argsPath string) {
	t.Helper()

	bin = t.TempDir()
	argsPath = filepath.Join(t.TempDir(), "args.txt")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$PWD\" \"$@\" > " + argsPath + "\n" +
		"exit " + strconv.Itoa(exitCode) + "\n"
	if err := os.WriteFile(filepath.Join(bin, "tp"), []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(tp): %v", err)
	}
	return bin, argsPath
}

func TestTpInvocations(t *testing.T) {
	tests := []struct {
		name     string
		wantArgv string
		mode     git.RemoveMode
		create   bool
		exitCode int
		wantErr  string
	}{
		{name: "New passes the base", wantArgv: "new cc-1-first --base origin/main", create: true, wantErr: "<nil>"},
		{name: "Remove merged", wantArgv: "remove --merged cc-1-first", mode: git.RemoveMerged, wantErr: "<nil>"},
		{name: "Remove forced", wantArgv: "remove --force cc-1-first", mode: git.RemoveForced, wantErr: "<nil>"},
		{name: "New failure", wantArgv: "new cc-1-first --base origin/main", create: true, exitCode: 1,
			wantErr: "create worktree cc-1-first: tp new cc-1-first --base origin/main in {dir}: exit status 1: "},
		{name: "Remove merged failure", wantArgv: "remove --merged cc-1-first", mode: git.RemoveMerged, exitCode: 1,
			wantErr: "remove worktree cc-1-first: tp remove --merged cc-1-first in {dir}: exit status 1: "},
		{name: "Remove forced failure", wantArgv: "remove --force cc-1-first", mode: git.RemoveForced, exitCode: 1,
			wantErr: "remove worktree cc-1-first: tp remove --force cc-1-first in {dir}: exit status 1: "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			bin, argsPath := fakeTp(t, tt.exitCode)
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			var err error
			if tt.create {
				err = (git.CLI{}).New(t.Context(), dir, "cc-1-first", "origin/main")
			} else {
				err = (git.CLI{}).Remove(t.Context(), dir, "cc-1-first", tt.mode)
			}
			want := strings.ReplaceAll(tt.wantErr, "{dir}", dir)
			if got := fmt.Sprint(err); got != want {
				t.Errorf("CLI(%s) = %q, want %q", tt.name, got, want)
			}
			var cmdErr *command.Error
			if got, want := errors.As(err, &cmdErr), tt.exitCode != 0; got != want {
				t.Errorf("CLI(%s) command error = %v, want %v", tt.name, got, want)
			}
			got, err := os.ReadFile(argsPath)
			if err != nil {
				t.Fatalf("read recorded args: %v", err)
			}
			lines := strings.Split(strings.TrimRight(string(got), "\n"), "\n")
			wantDir, err := filepath.EvalSymlinks(dir)
			if err != nil {
				t.Fatalf("EvalSymlinks(repoPath): %v", err)
			}
			gotDir, err := filepath.EvalSymlinks(lines[0])
			if err != nil {
				t.Fatalf("EvalSymlinks(recorded directory): %v", err)
			}
			if gotDir != wantDir {
				t.Errorf("cmd.Dir = %q, want %q", gotDir, wantDir)
			}
			if gotArgv := strings.Join(lines[1:], " "); gotArgv != tt.wantArgv {
				t.Errorf("argv = %q, want %q", gotArgv, tt.wantArgv)
			}
		})
	}
}

func TestRemoveUnknownMode(t *testing.T) {
	t.Parallel()
	err := (git.CLI{}).Remove(t.Context(), t.TempDir(), "cc-1", git.RemoveUnknown)
	if err == nil {
		t.Fatal("Remove(RemoveUnknown) = nil, want error")
	}
}
