package git_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/git"
	"github.com/O-Marsters-1997/command-center/internal/gittest"
)

func TestRemovalStateFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func(t *testing.T) (dir string, provenTips []string)
		want  git.RemovalState
	}{
		{
			name: "removable by merged while the remote ref still resolves",
			setup: func(t *testing.T) (string, []string) {
				t.Helper()
				dir, _ := gittest.WithOrigin(t)
				gittest.Run(t, dir, "commit", "-q", "--allow-empty", "-m", "not yet pushed")
				return dir, nil
			},
			want: git.RemovableByMerged,
		},
		{
			name: "not removable with no remote-tracking ref at all",
			setup: func(t *testing.T) (string, []string) {
				t.Helper()
				return gittest.Init(t), nil
			},
			want: git.NotRemovable,
		},
		{
			name: "removable by force when the ref is gone but the tip was recorded as pushed",
			setup: func(t *testing.T) (string, []string) {
				t.Helper()
				dir, _ := gittest.WithOrigin(t)
				tip, err := git.RevParse(t.Context(), dir, "refs/heads/main")
				if err != nil {
					t.Fatal(err)
				}
				pruneRemoteTrackingRef(t, dir, "main")
				return dir, []string{tip}
			},
			want: git.RemovableByForce,
		},
		{
			name: "removable by force at a second proven tip when the recorded push is stale",
			setup: func(t *testing.T) (string, []string) {
				t.Helper()
				dir, _ := gittest.WithOrigin(t)
				tip, err := git.RevParse(t.Context(), dir, "refs/heads/main")
				if err != nil {
					t.Fatal(err)
				}
				pruneRemoteTrackingRef(t, dir, "main")
				return dir, []string{"stale", tip}
			},
			want: git.RemovableByForce,
		},
		{
			name: "not removable when the ref is gone and the tip moved past the recorded push",
			setup: func(t *testing.T) (string, []string) {
				t.Helper()
				dir, _ := gittest.WithOrigin(t)
				tip, err := git.RevParse(t.Context(), dir, "refs/heads/main")
				if err != nil {
					t.Fatal(err)
				}
				pruneRemoteTrackingRef(t, dir, "main")
				gittest.Run(t, dir, "commit", "-q", "--allow-empty", "-m", "committed after the merge, never pushed")
				return dir, []string{tip}
			},
			want: git.NotRemovable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir, provenTips := tt.setup(t)
			got, err := git.RemovalStateFor(t.Context(), dir, "main", provenTips...)
			if err != nil {
				t.Fatalf("RemovalStateFor: %v", err)
			}
			if got != tt.want {
				t.Errorf("state = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDirty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(t *testing.T, dir string)
		want   bool
	}{
		{name: "a clean worktree", mutate: func(*testing.T, string) {}, want: false},
		{
			name: "an uncommitted change",
			mutate: func(t *testing.T, dir string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(dir, "scratch.txt"), []byte("x\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := gittest.Init(t)
			tt.mutate(t, dir)
			got, err := git.Dirty(t.Context(), dir)
			if err != nil {
				t.Fatalf("Dirty: %v", err)
			}
			if got != tt.want {
				t.Errorf("Dirty = %v, want %v", got, tt.want)
			}
		})
	}
}

func pruneRemoteTrackingRef(t *testing.T, repoPath, branch string) {
	t.Helper()
	gittest.Run(t, repoPath, "update-ref", "-d", "refs/remotes/origin/"+branch)
}

// TestWorktreePathsKeepsAMidRebaseWorktreeUnderItsBranch covers issue #91: git calls a rebasing
// worktree "detached", which dropped it from Observation.Worktrees and Observation.MidMerge
// alike, so the row read review_me while its worktree sat on an unresolved conflict.
func TestWorktreePathsKeepsAMidRebaseWorktreeUnderItsBranch(t *testing.T) {
	t.Parallel()

	repo, worktree := repoWithAConflictedRebase(t)

	worktrees, err := git.WorktreePaths(t.Context(), repo)
	if err != nil {
		t.Fatalf("WorktreePaths: %v", err)
	}
	if got := worktrees["feature"]; got != worktree {
		t.Errorf("WorktreePaths()[%q] = %q, want %q", "feature", got, worktree)
	}

	mid, err := git.MidMerge(t.Context(), worktree)
	if err != nil {
		t.Fatalf("MidMerge: %v", err)
	}
	if !mid {
		t.Error("MidMerge reported a worktree stopped on a rebase conflict as resolved")
	}
}

// repoWithAConflictedRebase leaves worktree stopped on a rebase conflict, the state a restack
// onto a squash-merged base lands in when both branches touched the same lines (issue #85).
func repoWithAConflictedRebase(t *testing.T) (repo, worktree string) {
	t.Helper()
	repo = gittest.Init(t)
	// git worktree list reports the symlink-resolved path, which on macOS is /private/var/...
	// for a /var/... TempDir, so the expectation has to be resolved the same way.
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	worktree = filepath.Join(tmp, "feature")

	gittest.Commit(t, repo, "shared.txt", "base\n")
	base, err := git.RevParse(t.Context(), repo, "HEAD")
	if err != nil {
		t.Fatalf("RevParse: %v", err)
	}

	gittest.Commit(t, repo, "shared.txt", "main side\n")

	gittest.Run(t, repo, "worktree", "add", "-q", "-b", "feature", worktree, base)
	gittest.Commit(t, worktree, "shared.txt", "feature side\n")

	if out, err := exec.Command("git", "-C", worktree, "rebase", "--onto", "main", base).CombinedOutput(); err == nil {
		t.Fatalf("rebase --onto main %s succeeded, want a conflict: %s", base, out)
	}
	return repo, worktree
}

// TestMidMergeClearsOnceAResolvedRebaseFinishes covers issue #91's other half. git leaves
// REBASE_HEAD behind after rebase --continue, so reading that ref pinned the row to
// refresh_conflicted for good, and the abort it then offered failed with "no rebase in progress".
func TestMidMergeClearsOnceAResolvedRebaseFinishes(t *testing.T) {
	t.Parallel()

	_, worktree := repoWithAConflictedRebase(t)
	resolveRebase(t, worktree)

	mid, err := git.MidMerge(t.Context(), worktree)
	if err != nil {
		t.Fatalf("MidMerge: %v", err)
	}
	if mid {
		t.Error("MidMerge still reports mid-rebase after the rebase finished, so the row can never " +
			"leave refresh_conflicted and refreshOne's own guard never lets it advance again")
	}
}

// resolveRebase does what a human does at the conflict and nothing else: write the file, add it,
// continue.
func resolveRebase(t *testing.T, worktree string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(worktree, "shared.txt"), []byte("both sides\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gittest.Run(t, worktree, "add", "shared.txt")
	gittest.Run(t, worktree, "-c", "core.editor=true", "rebase", "--continue")
}

func TestRemovalStateForAPathThatIsNotARepoIsAnErrorNotRemovable(t *testing.T) {
	t.Parallel()

	got, err := git.RemovalStateFor(t.Context(), t.TempDir(), "main")
	if err == nil {
		t.Fatalf("RemovalStateFor(not a repo) = %v, nil; want an error", got)
	}
	if got != git.RemovalUnknown {
		t.Errorf("state = %v, want RemovalUnknown", got)
	}
}

func TestDeleteBranchIfExistsOnAPathThatIsNotARepoIsAnError(t *testing.T) {
	t.Parallel()

	if err := git.DeleteBranchIfExists(t.Context(), t.TempDir(), "main"); err == nil {
		t.Error("DeleteBranchIfExists(not a repo) = nil, want an error")
	}
}

func TestTipIfPresent(t *testing.T) {
	t.Parallel()

	dir := gittest.Init(t)
	want, err := git.RevParse(t.Context(), dir, "refs/heads/main")
	if err != nil {
		t.Fatal(err)
	}

	got, present, err := git.TipIfPresent(t.Context(), dir, "refs/heads/main")
	if err != nil || !present || got != want {
		t.Errorf("TipIfPresent(main) = %q, %v, %v; want %q, true, nil", got, present, err, want)
	}

	_, present, err = git.TipIfPresent(t.Context(), dir, "refs/heads/absent")
	if err != nil || present {
		t.Errorf("TipIfPresent(absent) = _, %v, %v; want false, nil", present, err)
	}

	if _, _, err := git.TipIfPresent(t.Context(), t.TempDir(), "refs/heads/main"); err == nil {
		t.Error("TipIfPresent(not a repo) = nil error, want one")
	}
}
