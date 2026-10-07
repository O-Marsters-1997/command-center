package git_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/git"
)

func fakeTp(t *testing.T, exitCode int) (argsPath string) {
	t.Helper()

	bin := t.TempDir()
	argsPath = filepath.Join(t.TempDir(), "args.txt")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$PWD\" \"$@\" > " + argsPath + "\n" +
		"exit " + itoa(exitCode) + "\n"
	if err := os.WriteFile(filepath.Join(bin, "tp"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return argsPath
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	return "1"
}

func TestTpInvocations(t *testing.T) {
	tests := []struct {
		name     string
		call     func(t *testing.T, repoPath string) error
		wantArgv string
	}{
		{
			name: "New passes the base",
			call: func(t *testing.T, repoPath string) error {
				return (git.CLI{}).New(t.Context(), repoPath, "cc-1-first", "origin/main")
			},
			wantArgv: "new cc-1-first --base origin/main",
		},
		{
			name: "Remove merged",
			call: func(t *testing.T, repoPath string) error {
				return (git.CLI{}).Remove(t.Context(), repoPath, "cc-1-first", git.RemoveMerged)
			},
			wantArgv: "remove --merged cc-1-first",
		},
		{
			name: "Remove forced",
			call: func(t *testing.T, repoPath string) error {
				return (git.CLI{}).Remove(t.Context(), repoPath, "cc-1-first", git.RemoveForced)
			},
			wantArgv: "remove --force cc-1-first",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repoPath := t.TempDir()
			argsPath := fakeTp(t, 0)

			if err := tt.call(t, repoPath); err != nil {
				t.Fatalf("call: %v", err)
			}

			got, err := os.ReadFile(argsPath)
			if err != nil {
				t.Fatalf("read recorded args: %v", err)
			}
			lines := strings.Split(strings.TrimRight(string(got), "\n"), "\n")
			wantDir, err := filepath.EvalSymlinks(repoPath)
			if err != nil {
				t.Fatal(err)
			}
			gotDir, err := filepath.EvalSymlinks(lines[0])
			if err != nil {
				t.Fatal(err)
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

func TestTpFailureNamesTheBranch(t *testing.T) {
	tests := []struct {
		name string
		call func(t *testing.T, repoPath string) error
	}{
		{"New", func(t *testing.T, repoPath string) error {
			return (git.CLI{}).New(t.Context(), repoPath, "cc-1-first", "origin/main")
		}},
		{"Remove", func(t *testing.T, repoPath string) error {
			return (git.CLI{}).Remove(t.Context(), repoPath, "cc-1-first", git.RemoveMerged)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeTp(t, 1)

			err := tt.call(t, t.TempDir())
			if err == nil {
				t.Fatal("returned nil for a failing tp")
			}
			if !strings.Contains(err.Error(), "cc-1-first") {
				t.Errorf("error %q does not name the branch", err)
			}
		})
	}
}
