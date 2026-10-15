package git_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/command"
	"github.com/O-Marsters-1997/command-center/internal/git"
	"github.com/O-Marsters-1997/command-center/internal/tp"
)

func fakeTp(t *testing.T, exitCode int) (bin, argsPath string) {
	t.Helper()

	bin = t.TempDir()
	argsPath = filepath.Join(t.TempDir(), "args.txt")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$PWD\" \"$@\" > " + argsPath + "\n" +
		"exit " + strconv.Itoa(exitCode) + "\n"
	if err := os.WriteFile(filepath.Join(bin, "tp"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, argsPath
}

func TestTpInvocations(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		wantArgv string
	}{
		{name: "New passes the base", wantArgv: "new cc-1-first --base origin/main"},
		{name: "Remove merged", wantArgv: "remove --merged cc-1-first"},
		{name: "Remove forced", wantArgv: "remove --force cc-1-first"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			repoPath := t.TempDir()
			bin, argsPath := fakeTp(t, 0)
			runTpChild(t, tpChild{bin: bin, repoPath: repoPath, action: tt.name})

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
	t.Parallel()
	for _, action := range []string{"New", "Remove", "Remove forced"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			bin, _ := fakeTp(t, 1)
			runTpChild(t, tpChild{bin: bin, repoPath: t.TempDir(), action: action, failing: true})
		})
	}
}

type tpChild struct {
	bin, repoPath, action string
	failing               bool
}

func runTpChild(t *testing.T, child tpChild) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestTpChild$")
	cmd.Env = append(os.Environ(),
		"PATH="+child.bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"CC_TEST_TP_ACTION="+child.action,
		"CC_TEST_TP_REPO="+child.repoPath,
		"CC_TEST_TP_FAIL="+strconv.FormatBool(child.failing),
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tp child: %v: %s", err, out)
	}
}

func TestTpChild(t *testing.T) {
	action := os.Getenv("CC_TEST_TP_ACTION")
	if action == "" {
		return
	}
	repo := os.Getenv("CC_TEST_TP_REPO")
	var err error
	switch action {
	case "New", "New passes the base":
		err = (git.CLI{}).New(t.Context(), repo, "cc-1-first", "origin/main")
	case "Remove", "Remove merged":
		err = (git.CLI{}).Remove(t.Context(), repo, "cc-1-first", tp.RemoveMerged)
	case "Remove forced":
		err = (git.CLI{}).Remove(t.Context(), repo, "cc-1-first", tp.RemoveForced)
	default:
		t.Fatalf("unknown tp action %q", action)
	}
	if os.Getenv("CC_TEST_TP_FAIL") == "true" {
		var cmdErr *command.Error
		if !errors.As(err, &cmdErr) {
			t.Fatalf("CLI(%s) = %v, want *command.Error", action, err)
		}
		want := "tp new cc-1-first --base origin/main"
		switch action {
		case "Remove":
			want = "tp remove --merged cc-1-first"
		case "Remove forced":
			want = "tp remove --force cc-1-first"
		}
		if cmdErr.Cmd != want || cmdErr.Dir != repo {
			t.Errorf("CLI(%s) command = %q in %q, want %q in %q", action, cmdErr.Cmd, cmdErr.Dir, want, repo)
		}
	} else if err != nil {
		t.Errorf("CLI(%s) = %v, want nil", action, err)
	}
}
