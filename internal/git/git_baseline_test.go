package git_test

import (
	"testing"

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

func TestCommitsSinceCountsCommitsAfterBaseline(t *testing.T) {
	t.Parallel()

	dir := gittest.Init(t)
	baseline, err := git.BranchTip(t.Context(), dir, "main")
	if err != nil {
		t.Fatal(err)
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
