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
		setup    func(*testing.T, string) string
		remote   func(root string) string
		wantName string
		wantOK   bool
	}{
		{name: "root", setup: rootCheckout, remote: localRemote, wantName: "r", wantOK: true},
		{name: "subdirectory", setup: func(t *testing.T, repo string) string {
			t.Helper()
			dir := filepath.Join(repo, "sub")
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatalf("Mkdir(%q): %v", dir, err)
			}
			return dir
		}, remote: localRemote, wantName: "r", wantOK: true},
		{name: "worktree", setup: func(t *testing.T, repo string) string {
			t.Helper()
			dir := filepath.Join(filepath.Dir(repo), "worktree")
			gittest.Run(t, repo, "worktree", "add", dir)
			return dir
		}, remote: localRemote, wantName: "r", wantOK: true},
		{name: "alternate remote", setup: func(t *testing.T, repo string) string {
			t.Helper()
			gittest.Run(t, repo, "remote", "set-url", "origin", "git@github.com:o/r.git")
			return repo
		}, remote: func(string) string { return "https://github.com/o/r" }, wantName: "r", wantOK: true},
		{name: "without origin", setup: func(t *testing.T, _ string) string {
			t.Helper()
			return t.TempDir()
		}, remote: localRemote},
		{
			name: "no matching repo", setup: rootCheckout,
			remote: func(string) string { return "git@github.com:o/other.git" },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root, repoPath := repoWithOrigin(t)
			dir := tt.setup(t, repoPath)
			repos := []store.Repo{{Name: "r", Remote: tt.remote(root)}}
			name, ok := loop.RepoNameForDir(t.Context(), dir, repos)
			if ok != tt.wantOK || name != tt.wantName {
				t.Errorf("RepoNameForDir(%q) = %q, %v, want %q, %v", dir, name, ok, tt.wantName, tt.wantOK)
			}
		})
	}
}

func localRemote(root string) string { return filepath.Join(root, "remote.git") }

func rootCheckout(t *testing.T, repo string) string {
	t.Helper()
	return repo
}
