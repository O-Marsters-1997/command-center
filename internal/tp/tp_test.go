package tp_test

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/command"
	"github.com/O-Marsters-1997/command-center/internal/tp"
	"github.com/O-Marsters-1997/command-center/internal/tp/tptest"
)

type ran struct{ dir, line string }

func recordingTP(runs *[]ran, err error) tp.CLI {
	run := func(_ context.Context, _ time.Duration, dir, name string, args ...string) ([]byte, error) {
		*runs = append(*runs, ran{dir: dir, line: name + " " + strings.Join(args, " ")})
		return nil, err
	}
	return tp.CLI{Exec: command.ExecutorFunc(run)}
}

func TestCLI(t *testing.T) {
	t.Parallel()
	bin := filepath.Join(t.TempDir(), "tp")
	if out, err := exec.Command("go", "build", "-o", bin, "../../e2e/faketp").CombinedOutput(); err != nil {
		t.Fatalf("build fake tp: %v: %s", err, out)
	}
	run := func(ctx context.Context, timeout time.Duration, dir, _ string, args ...string) ([]byte, error) {
		return command.OutputWithin(ctx, timeout, dir, bin, args...)
	}
	tptest.RunContract(t, func(*testing.T) tp.Worktrees { return tp.CLI{Exec: command.ExecutorFunc(run)} })
}

func TestCLI_New(t *testing.T) {
	t.Parallel()

	t.Run("cuts the branch from the base in the repo", func(t *testing.T) {
		t.Parallel()
		var runs []ran
		if err := recordingTP(&runs, nil).New(t.Context(), "/repo", "cc-1-first", "origin/main"); err != nil {
			t.Fatalf("New() error = %v", err)
		}
		want := []ran{{dir: "/repo", line: "tp new cc-1-first --base origin/main"}}
		if len(runs) != 1 || runs[0] != want[0] {
			t.Errorf("New(cc-1-first) ran %+v, want %+v", runs, want)
		}
	})

	t.Run("a tp failure is returned", func(t *testing.T) {
		t.Parallel()
		var runs []ran
		errTP := errors.New("tp exited 1")
		err := recordingTP(&runs, errTP).New(t.Context(), "/repo", "cc-1-first", "origin/main")
		if !errors.Is(err, errTP) {
			t.Errorf("New(cc-1-first) error = %v, want %v", err, errTP)
		}
	})
}

func TestCLI_Remove(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		mode tp.RemoveMode
		want string
	}{
		{name: "merged", mode: tp.RemoveMerged, want: "tp remove --merged cc-1-first"},
		{name: "forced", mode: tp.RemoveForced, want: "tp remove --force cc-1-first"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var runs []ran
			if err := recordingTP(&runs, nil).Remove(t.Context(), "/repo", "cc-1-first", tt.mode); err != nil {
				t.Fatalf("Remove() error = %v", err)
			}
			want := ran{dir: "/repo", line: tt.want}
			if len(runs) != 1 || runs[0] != want {
				t.Errorf("Remove(%s) ran %+v, want %+v", tt.name, runs, want)
			}
		})
	}

	t.Run("an unknown mode runs nothing", func(t *testing.T) {
		t.Parallel()
		var runs []ran
		err := recordingTP(&runs, nil).Remove(t.Context(), "/repo", "cc-1", tp.RemoveUnknown)
		if err == nil || len(runs) != 0 {
			t.Errorf("Remove(RemoveUnknown) = %v after running %+v, want an error and no run", err, runs)
		}
	})

	t.Run("a tp failure is returned", func(t *testing.T) {
		t.Parallel()
		var runs []ran
		errTP := errors.New("tp exited 1")
		err := recordingTP(&runs, errTP).Remove(t.Context(), "/repo", "cc-1-first", tp.RemoveMerged)
		if !errors.Is(err, errTP) {
			t.Errorf("Remove(cc-1-first) error = %v, want %v", err, errTP)
		}
	})
}
