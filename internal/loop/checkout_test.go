package loop_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/gittest"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

func TestRepoNameForDir(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		remote   func(root string) string
		wantName string
		wantOK   bool
	}{
		{name: "root", remote: localRemote, wantName: "r", wantOK: true},
		{
			name: "no matching repo", remote: func(string) string { return "git@github.com:o/other.git" },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root, repoPath := repoWithOrigin(t)
			repos := []store.Repo{{Name: "r", Remote: tt.remote(root)}}
			name, ok := loop.RepoNameForDir(t.Context(), repoPath, repos)
			if ok != tt.wantOK || name != tt.wantName {
				t.Errorf("RepoNameForDir(%q) = %q, %v, want %q, %v", repoPath, name, ok, tt.wantName, tt.wantOK)
			}
		})
	}
}

func TestRepoNameForSubdirectory(t *testing.T) {
	t.Parallel()
	root, repoPath := repoWithOrigin(t)
	dir := filepath.Join(repoPath, "sub")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("Mkdir(%q): %v", dir, err)
	}
	name, ok := loop.RepoNameForDir(t.Context(), dir, []store.Repo{{Name: "r", Remote: localRemote(root)}})
	if name != "r" || !ok {
		t.Errorf("RepoNameForDir(%q) = %q, %v, want r, true", dir, name, ok)
	}
}

func TestRepoNameForWorktree(t *testing.T) {
	t.Parallel()
	root, repoPath := repoWithOrigin(t)
	dir := filepath.Join(filepath.Dir(repoPath), "worktree")
	gittest.Run(t, repoPath, "worktree", "add", dir)
	name, ok := loop.RepoNameForDir(t.Context(), dir, []store.Repo{{Name: "r", Remote: localRemote(root)}})
	if name != "r" || !ok {
		t.Errorf("RepoNameForDir(%q) = %q, %v, want r, true", dir, name, ok)
	}
}

func TestRepoNameForAlternateRemote(t *testing.T) {
	t.Parallel()
	_, repoPath := repoWithOrigin(t)
	gittest.Run(t, repoPath, "remote", "set-url", "origin", "git@github.com:o/r.git")
	repos := []store.Repo{{Name: "r", Remote: "https://github.com/o/r"}}
	name, ok := loop.RepoNameForDir(t.Context(), repoPath, repos)
	if name != "r" || !ok {
		t.Errorf("RepoNameForDir(%q) = %q, %v, want r, true", repoPath, name, ok)
	}
}

func TestRepoNameForDirectoryWithoutOrigin(t *testing.T) {
	t.Parallel()
	root, _ := repoWithOrigin(t)
	dir := t.TempDir()
	name, ok := loop.RepoNameForDir(t.Context(), dir, []store.Repo{{Name: "r", Remote: localRemote(root)}})
	if name != "" || ok {
		t.Errorf("RepoNameForDir(%q) = %q, %v, want empty name, false", dir, name, ok)
	}
}

func localRemote(root string) string { return filepath.Join(root, "remote.git") }
