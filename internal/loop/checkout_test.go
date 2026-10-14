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
		dir      func(t *testing.T, repoPath string) string
		wantName string
		wantOK   bool
	}{
		{
			name: "root", remote: localRemote,
			dir:      func(_ *testing.T, repo string) string { return repo },
			wantName: "r", wantOK: true,
		},
		{
			name: "subdirectory", remote: localRemote,
			dir: func(t *testing.T, repo string) string {
				t.Helper()
				sub := filepath.Join(repo, "sub")
				if err := os.Mkdir(sub, 0o700); err != nil {
					t.Fatal(err)
				}
				return sub
			},
			wantName: "r", wantOK: true,
		},
		{
			name: "worktree", remote: localRemote,
			dir: func(t *testing.T, repo string) string {
				t.Helper()
				worktree := filepath.Join(filepath.Dir(repo), "worktree")
				gittest.Run(t, repo, "worktree", "add", worktree)
				return worktree
			},
			wantName: "r", wantOK: true,
		},
		{
			name:   "the other URL form of the same remote",
			remote: func(string) string { return "https://github.com/o/r" },
			dir: func(t *testing.T, repo string) string {
				t.Helper()
				gittest.Run(t, repo, "remote", "set-url", "origin", "git@github.com:o/r.git")
				return repo
			},
			wantName: "r", wantOK: true,
		},
		{
			name: "no matching repo", remote: func(string) string { return "git@github.com:o/other.git" },
			dir: func(_ *testing.T, repo string) string { return repo },
		},
		{
			name: "no origin", remote: localRemote,
			dir: func(t *testing.T, _ string) string {
				t.Helper()
				return t.TempDir()
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root, repoPath := repoWithOrigin(t)
			repos := []store.Repo{{Name: "r", Remote: tt.remote(root)}}

			name, ok := loop.RepoNameForDir(t.Context(), tt.dir(t, repoPath), repos)
			if ok != tt.wantOK || name != tt.wantName {
				t.Errorf("RepoNameForDir = %q, %v, want %q, %v", name, ok, tt.wantName, tt.wantOK)
			}
		})
	}
}

func localRemote(root string) string { return filepath.Join(root, "remote.git") }
