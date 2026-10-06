package cc_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/cc"
)

func TestCheckoutPathRefusesBothFormsOrNeither(t *testing.T) {
	t.Parallel()

	both := "[[repo]]\nname = \"r\"\nremote = \"git@github.com:o/r.git\"\npath = \"r\"\n"
	if _, err := cc.LoadConfig(writeConfig(t, both)); err == nil ||
		!strings.Contains(err.Error(), "r") || !strings.Contains(err.Error(), "both") {
		t.Errorf("error = %v, want one naming the repo that sets both remote and path", err)
	}

	neither := "[[repo]]\nname = \"r\"\n"
	if _, err := cc.LoadConfig(writeConfig(t, neither)); err == nil || !strings.Contains(err.Error(), "neither") {
		t.Errorf("error = %v, want one naming the repo that sets neither", err)
	}
}

func TestLoadConfigPutsARemoteRepoUnderTheDataDir(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("CC_DATA_DIR", dataDir)

	got, err := cc.LoadConfig(writeConfig(t,
		"[[repo]]\nname = \"command-center\"\nremote = \"git@github.com:o/command-center.git\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dataDir, "repos", "command-center"); got.Repos[0].Checkout != want {
		t.Errorf("checkout = %q, want %q", got.Repos[0].Checkout, want)
	}
}

func TestLoadConfigRefusesARemoteRepoNamedLikeAPath(t *testing.T) {
	t.Setenv("CC_DATA_DIR", t.TempDir())

	_, err := cc.LoadConfig(writeConfig(t,
		"[[repo]]\nname = \"../escape\"\nremote = \"git@github.com:o/r.git\"\n"))
	if err == nil || !strings.Contains(err.Error(), "escape") {
		t.Errorf("error = %v, want one refusing a repo name that is not a single directory", err)
	}
}

func TestRepoNameForDirMatchesFromRootSubdirAndWorktree(t *testing.T) {
	_, repoPath := repoWithOrigin(t)
	remote := filepath.Join(filepath.Dir(repoPath), "remote.git")
	repos := []cc.Repo{{Name: "r", Remote: remote}}

	if name, ok := cc.RepoNameForDir(t.Context(), repoPath, repos); !ok || name != "r" {
		t.Errorf("root: RepoNameForDir = %q, %v, want %q, true", name, ok, "r")
	}

	sub := filepath.Join(repoPath, "sub")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	if name, ok := cc.RepoNameForDir(t.Context(), sub, repos); !ok || name != "r" {
		t.Errorf("subdir: RepoNameForDir = %q, %v, want %q, true", name, ok, "r")
	}

	worktree := filepath.Join(filepath.Dir(repoPath), "worktree")
	runGit(t, "-C", repoPath, "worktree", "add", worktree)
	if name, ok := cc.RepoNameForDir(t.Context(), worktree, repos); !ok || name != "r" {
		t.Errorf("worktree: RepoNameForDir = %q, %v, want %q, true", name, ok, "r")
	}
}

func TestRepoNameForDirResolvesTheOtherURLFormToTheSameName(t *testing.T) {
	_, repoPath := repoWithOrigin(t)
	runGit(t, "-C", repoPath, "remote", "set-url", "origin", "git@github.com:o/r.git")

	repos := []cc.Repo{{Name: "r", Remote: "https://github.com/o/r"}}
	if name, ok := cc.RepoNameForDir(t.Context(), repoPath, repos); !ok || name != "r" {
		t.Errorf("RepoNameForDir = %q, %v, want %q, true", name, ok, "r")
	}
}

func TestRepoNameForDirAnswersNotOKForNoOriginOrNoMatch(t *testing.T) {
	if name, ok := cc.RepoNameForDir(t.Context(), t.TempDir(), nil); ok {
		t.Errorf("no origin: RepoNameForDir = %q, %v, want ok=false", name, ok)
	}

	_, repoPath := repoWithOrigin(t)
	repos := []cc.Repo{{Name: "other", Remote: "git@github.com:o/other.git"}}
	if name, ok := cc.RepoNameForDir(t.Context(), repoPath, repos); ok {
		t.Errorf("no match: RepoNameForDir = %q, %v, want ok=false", name, ok)
	}
}
