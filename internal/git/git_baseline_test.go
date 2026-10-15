package git_test

import (
	"errors"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/command"
	"github.com/O-Marsters-1997/command-center/internal/git"
	"github.com/O-Marsters-1997/command-center/internal/gittest"
)

func TestBranchTipReadsTheCurrentTip(t *testing.T) {
	t.Parallel()

	dir := gittest.Init(t)
	want := gittest.Run(t, dir, "rev-parse", "refs/heads/main")

	got, err := git.BranchTip(t.Context(), dir, "main")
	if err != nil {
		t.Fatalf("BranchTip: %v", err)
	}
	if got == "" || got != want {
		t.Errorf("BranchTip = %q, want %q", got, want)
	}
}

func TestRevParsePreservesCommandError(t *testing.T) {
	t.Parallel()

	_, err := git.RevParse(t.Context(), gittest.Init(t), "refs/heads/missing")
	var commandErr *command.Error
	if !errors.As(err, &commandErr) {
		t.Fatalf("RevParse(missing) error = %v, want *command.Error", err)
	}
}

func TestCommitsSinceCountsCommitsAfterBaseline(t *testing.T) {
	t.Parallel()

	dir := gittest.Init(t)
	baseline, err := git.BranchTip(t.Context(), dir, "main")
	if err != nil {
		t.Fatalf("BranchTip(main): %v", err)
	}

	n, err := git.CommitsSince(t.Context(), dir, baseline, "HEAD")
	if err != nil {
		t.Fatalf("CommitsSince: %v", err)
	}
	if n != 0 {
		t.Errorf("commits since own tip = %d, want 0", n)
	}

	gittest.Run(t, dir, "commit", "-q", "--allow-empty", "-m", "agent commit")
	n, err = git.CommitsSince(t.Context(), dir, baseline, "HEAD")
	if err != nil {
		t.Fatalf("CommitsSince: %v", err)
	}
	if n != 1 {
		t.Errorf("commits since baseline = %d, want 1", n)
	}
}
