package git_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/adapter/command"
	"github.com/O-Marsters-1997/command-center/internal/adapter/git"
	"github.com/O-Marsters-1997/command-center/internal/adapter/git/gittest"
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
					t.Fatalf("RevParse(refs/heads/main): %v", err)
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
					t.Fatalf("RevParse(refs/heads/main): %v", err)
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
					t.Fatalf("RevParse(refs/heads/main): %v", err)
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
				t.Fatalf("RemovalStateFor(main, %v) error = %v", provenTips, err)
			}
			if got != tt.want {
				t.Errorf("RemovalStateFor(main, %v) = %v, want %v", provenTips, got, tt.want)
			}
		})
	}

	t.Run("a path that is not a repo is an error, not removable", func(t *testing.T) {
		t.Parallel()
		got, err := git.RemovalStateFor(t.Context(), t.TempDir(), "main")
		if err == nil || got != git.RemovalUnknown {
			t.Errorf("RemovalStateFor(not a repo) = %v, %v, want RemovalUnknown and an error", got, err)
		}
	})
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
					t.Fatalf("WriteFile(scratch.txt): %v", err)
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
				t.Fatalf("Dirty(%s) error = %v", tt.name, err)
			}
			if got != tt.want {
				t.Errorf("Dirty(%s) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func pruneRemoteTrackingRef(t *testing.T, repoPath, branch string) {
	t.Helper()
	gittest.Run(t, repoPath, "update-ref", "-d", "refs/remotes/origin/"+branch)
}

func TestWorktreePaths(t *testing.T) {
	t.Parallel()

	t.Run("keys each branch's worktree and drops a detached one", func(t *testing.T) {
		t.Parallel()
		repo := gittest.Init(t)
		tmp, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatalf("EvalSymlinks(TempDir): %v", err)
		}
		branch, detached := filepath.Join(tmp, "branch"), filepath.Join(tmp, "detached")
		gittest.Run(t, repo, "worktree", "add", "-q", "-b", "cc-1-first", branch)
		gittest.Run(t, repo, "worktree", "add", "-q", "--detach", detached)
		repoDir, err := filepath.EvalSymlinks(repo)
		if err != nil {
			t.Fatalf("EvalSymlinks(repo): %v", err)
		}

		got, err := git.WorktreePaths(t.Context(), repo)
		if err != nil {
			t.Fatalf("WorktreePaths() error = %v", err)
		}
		want := map[string]string{"main": repoDir, "cc-1-first": branch}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("WorktreePaths() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("keeps a mid-rebase worktree under its branch", func(t *testing.T) {
		t.Parallel()
		repo, worktree := repoWithAConflictedRebase(t)

		worktrees, err := git.WorktreePaths(t.Context(), repo)
		if err != nil {
			t.Fatalf("WorktreePaths() error = %v", err)
		}
		if got := worktrees["feature"]; got != worktree {
			t.Errorf("WorktreePaths()[feature] = %q, want %q", got, worktree)
		}
	})
}

func TestMidMerge(t *testing.T) {
	t.Parallel()

	t.Run("a worktree stopped on a rebase conflict", func(t *testing.T) {
		t.Parallel()
		_, worktree := repoWithAConflictedRebase(t)

		mid, err := git.MidMerge(t.Context(), worktree)
		if err != nil || !mid {
			t.Errorf("MidMerge(conflicted) = %v, %v, want true, nil", mid, err)
		}
	})

	t.Run("a resolved rebase that finished", func(t *testing.T) {
		t.Parallel()
		_, worktree := repoWithAConflictedRebase(t)
		resolveRebase(t, worktree)

		mid, err := git.MidMerge(t.Context(), worktree)
		if err != nil || mid {
			t.Errorf("MidMerge(resolved) = %v, %v, want false, nil", mid, err)
		}
	})
}

func repoWithAConflictedRebase(t *testing.T) (repo, worktree string) {
	t.Helper()
	repo = gittest.Init(t)
	// On macOS Git reports /private/var for a /var TempDir through the symlink.
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks(TempDir): %v", err)
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

func resolveRebase(t *testing.T, worktree string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(worktree, "shared.txt"), []byte("both sides\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(shared.txt): %v", err)
	}
	gittest.Run(t, worktree, "add", "shared.txt")
	gittest.Run(t, worktree, "-c", "core.editor=true", "rebase", "--continue")
}

func TestDeleteBranchIfExists(t *testing.T) {
	t.Parallel()

	if err := git.DeleteBranchIfExists(t.Context(), t.TempDir(), "main"); err == nil {
		t.Error("DeleteBranchIfExists(not a repo) error = nil, want one")
	}
}

func TestTipIfPresent(t *testing.T) {
	t.Parallel()

	t.Run("a present ref returns its tip", func(t *testing.T) {
		t.Parallel()
		dir := gittest.Init(t)
		want := gittest.Run(t, dir, "rev-parse", "refs/heads/main")

		got, present, err := git.TipIfPresent(t.Context(), dir, "refs/heads/main")
		if err != nil || !present || got != want {
			t.Errorf("TipIfPresent(main) = %q, %v, %v, want %q, true, nil", got, present, err, want)
		}
	})

	t.Run("an absent ref is not present", func(t *testing.T) {
		t.Parallel()
		_, present, err := git.TipIfPresent(t.Context(), gittest.Init(t), "refs/heads/absent")
		if err != nil || present {
			t.Errorf("TipIfPresent(absent) = _, %v, %v, want false, nil", present, err)
		}
	})

	t.Run("not a repo is an error", func(t *testing.T) {
		t.Parallel()
		if _, _, err := git.TipIfPresent(t.Context(), t.TempDir(), "refs/heads/main"); err == nil {
			t.Error("TipIfPresent(not a repo) error = nil, want one")
		}
	})
}

func TestBranchTip(t *testing.T) {
	t.Parallel()

	dir := gittest.Init(t)
	want := gittest.Run(t, dir, "rev-parse", "refs/heads/main")

	got, err := git.BranchTip(t.Context(), dir, "main")
	if err != nil {
		t.Fatalf("BranchTip(main) error = %v", err)
	}
	if got != want {
		t.Errorf("BranchTip(main) = %q, want %q", got, want)
	}
}

func TestRevParse(t *testing.T) {
	t.Parallel()

	_, err := git.RevParse(t.Context(), gittest.Init(t), "refs/heads/missing")
	if _, ok := errors.AsType[*command.Error](err); !ok {
		t.Errorf("RevParse(missing) error = %v, want *command.Error", err)
	}
}

func TestCommitsSince(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		commits int
	}{
		{name: "at the baseline", commits: 0},
		{name: "one commit after the baseline", commits: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := gittest.Init(t)
			baseline := gittest.Run(t, dir, "rev-parse", "HEAD")
			for range tt.commits {
				gittest.Run(t, dir, "commit", "-q", "--allow-empty", "-m", "agent commit")
			}

			got, err := git.CommitsSince(t.Context(), dir, baseline, "HEAD")
			if err != nil {
				t.Fatalf("CommitsSince() error = %v", err)
			}
			if got != tt.commits {
				t.Errorf("CommitsSince(baseline, HEAD) = %d, want %d", got, tt.commits)
			}
		})
	}
}

func TestMergesCleanly(t *testing.T) {
	t.Parallel()

	dir := gittest.Init(t)
	gittest.Commit(t, dir, "f.txt", "one\ntwo\nthree\n")
	gittest.Run(t, dir, "checkout", "-q", "-b", "child")
	gittest.Commit(t, dir, "f.txt", "one\nTWO-from-child\nthree\n")
	gittest.Run(t, dir, "checkout", "-q", "-b", "elsewhere", "main")
	gittest.Commit(t, dir, "f.txt", "one\ntwo\nthree\nfour\n")
	gittest.Run(t, dir, "checkout", "-q", "main")
	gittest.Commit(t, dir, "f.txt", "one\nTWO-from-main\nthree\n")

	tests := []struct {
		branch string
		want   bool
	}{
		{branch: "child", want: false},
		{branch: "elsewhere", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.branch, func(t *testing.T) {
			t.Parallel()
			got, err := git.MergesCleanly(t.Context(), dir, "main", tt.branch)
			if err != nil {
				t.Fatalf("MergesCleanly(main, %s) error = %v", tt.branch, err)
			}
			if got != tt.want {
				t.Errorf("MergesCleanly(main, %s) = %v, want %v", tt.branch, got, tt.want)
			}
		})
	}
}
