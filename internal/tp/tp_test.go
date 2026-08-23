package tp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/tp"
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

func TestNewInvokesTpNewWithBaseInTheRepoDir(t *testing.T) {
	repoPath := t.TempDir()
	argsPath := fakeTp(t, 0)

	if err := tp.New(t.Context(), repoPath, "cc-1-first", "origin/main"); err != nil {
		t.Fatalf("New: %v", err)
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
	if want := "new cc-1-first --base origin/main"; strings.Join(lines[1:], " ") != want {
		t.Errorf("argv = %q, want %q", strings.Join(lines[1:], " "), want)
	}
}

func TestNewReturnsAWrappedErrorOnFailure(t *testing.T) {
	repoPath := t.TempDir()
	fakeTp(t, 1)

	err := tp.New(t.Context(), repoPath, "cc-1-first", "origin/main")
	if err == nil {
		t.Fatal("New returned nil for a failing tp new")
	}
	if !strings.Contains(err.Error(), "cc-1-first") {
		t.Errorf("error %q does not name the branch", err)
	}
}

func TestRemoveInvokesTpRemoveWithForce(t *testing.T) {
	repoPath := t.TempDir()
	argsPath := fakeTp(t, 0)

	if err := tp.Remove(t.Context(), repoPath, "cc-1-first"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	got, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read recorded args: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(got), "\n"), "\n")
	if want := "remove --force cc-1-first"; strings.Join(lines[1:], " ") != want {
		t.Errorf("argv = %q, want %q", strings.Join(lines[1:], " "), want)
	}
}

func TestRemoveReturnsAWrappedErrorOnFailure(t *testing.T) {
	repoPath := t.TempDir()
	fakeTp(t, 1)

	err := tp.Remove(t.Context(), repoPath, "cc-1-first")
	if err == nil {
		t.Fatal("Remove returned nil for a failing tp remove")
	}
	if !strings.Contains(err.Error(), "cc-1-first") {
		t.Errorf("error %q does not name the branch", err)
	}
}
