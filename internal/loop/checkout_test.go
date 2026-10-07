package loop_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/loop"
)

func TestRepoNameForDirMatchesFromRootSubdirAndWorktree(t *testing.T) {
	_, repoPath := repoWithOrigin(t)
	remote := filepath.Join(filepath.Dir(repoPath), "remote.git")
	repos := []config.Repo{{Name: "r", Remote: remote}}

	if name, ok := loop.RepoNameForDir(t.Context(), repoPath, repos); !ok || name != "r" {
		t.Errorf("root: RepoNameForDir = %q, %v, want %q, true", name, ok, "r")
	}

	sub := filepath.Join(repoPath, "sub")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	if name, ok := loop.RepoNameForDir(t.Context(), sub, repos); !ok || name != "r" {
		t.Errorf("subdir: RepoNameForDir = %q, %v, want %q, true", name, ok, "r")
	}

	worktree := filepath.Join(filepath.Dir(repoPath), "worktree")
	runGit(t, "-C", repoPath, "worktree", "add", worktree)
	if name, ok := loop.RepoNameForDir(t.Context(), worktree, repos); !ok || name != "r" {
		t.Errorf("worktree: RepoNameForDir = %q, %v, want %q, true", name, ok, "r")
	}
}

func TestRepoNameForDirResolvesTheOtherURLFormToTheSameName(t *testing.T) {
	_, repoPath := repoWithOrigin(t)
	runGit(t, "-C", repoPath, "remote", "set-url", "origin", "git@github.com:o/r.git")

	repos := []config.Repo{{Name: "r", Remote: "https://github.com/o/r"}}
	if name, ok := loop.RepoNameForDir(t.Context(), repoPath, repos); !ok || name != "r" {
		t.Errorf("RepoNameForDir = %q, %v, want %q, true", name, ok, "r")
	}
}

func TestRepoNameForDirAnswersNotOKForNoOriginOrNoMatch(t *testing.T) {
	if name, ok := loop.RepoNameForDir(t.Context(), t.TempDir(), nil); ok {
		t.Errorf("no origin: RepoNameForDir = %q, %v, want ok=false", name, ok)
	}

	_, repoPath := repoWithOrigin(t)
	repos := []config.Repo{{Name: "other", Remote: "git@github.com:o/other.git"}}
	if name, ok := loop.RepoNameForDir(t.Context(), repoPath, repos); ok {
		t.Errorf("no match: RepoNameForDir = %q, %v, want ok=false", name, ok)
	}
}
