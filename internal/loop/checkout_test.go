package loop_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/adapter/git/gittest"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

func TestRepoNameForDir(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		dir      func(t *testing.T, root, repoPath string) string
		remote   func(root string) string
		wantName string
		wantOK   bool
	}{
		{
			name:   "the checkout root",
			dir:    func(_ *testing.T, _, repoPath string) string { return repoPath },
			remote: localRemote, wantName: "r", wantOK: true,
		},
		{
			name: "a subdirectory",
			dir: func(t *testing.T, _, repoPath string) string {
				t.Helper()
				dir := filepath.Join(repoPath, "sub")
				if err := os.Mkdir(dir, 0o700); err != nil {
					t.Fatalf("Mkdir(%q): %v", dir, err)
				}
				return dir
			},
			remote: localRemote, wantName: "r", wantOK: true,
		},
		{
			name: "a worktree",
			dir: func(t *testing.T, _, repoPath string) string {
				t.Helper()
				dir := filepath.Join(filepath.Dir(repoPath), "worktree")
				gittest.Run(t, repoPath, "worktree", "add", dir)
				return dir
			},
			remote: localRemote, wantName: "r", wantOK: true,
		},
		{
			name: "an alternate spelling of the remote",
			dir: func(t *testing.T, _, repoPath string) string {
				t.Helper()
				gittest.Run(t, repoPath, "remote", "set-url", "origin", "git@github.com:o/r.git")
				return repoPath
			},
			remote: func(string) string { return "https://github.com/o/r" }, wantName: "r", wantOK: true,
		},
		{
			name:   "no matching repo",
			dir:    func(_ *testing.T, _, repoPath string) string { return repoPath },
			remote: func(string) string { return "git@github.com:o/other.git" },
		},
		{
			name: "a directory with no origin",
			dir: func(t *testing.T, _, _ string) string {
				t.Helper()
				return t.TempDir()
			},
			remote: localRemote,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root, repoPath := repoWithOrigin(t)
			dir := tt.dir(t, root, repoPath)
			repos := []store.Repo{{Name: "r", Remote: tt.remote(root)}}

			name, ok := loop.RepoNameForDir(t.Context(), dir, repos)
			if name != tt.wantName || ok != tt.wantOK {
				t.Errorf("RepoNameForDir(%q) = %q, %v, want %q, %v", dir, name, ok, tt.wantName, tt.wantOK)
			}
		})
	}
}

func localRemote(root string) string { return filepath.Join(root, "remote.git") }
