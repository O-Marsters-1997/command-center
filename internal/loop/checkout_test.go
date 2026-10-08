package loop_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

func TestRepoNameForDir(t *testing.T) {
	tests := []struct {
		name      string
		remote    string
		setOrigin string
		dir       func(t *testing.T, repoPath string) string
		wantName  string
		wantOK    bool
	}{
		{name: "root", dir: func(_ *testing.T, repo string) string { return repo }, wantName: "r", wantOK: true},
		{
			name: "subdirectory",
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
			name: "worktree",
			dir: func(t *testing.T, repo string) string {
				t.Helper()
				worktree := filepath.Join(filepath.Dir(repo), "worktree")
				runGit(t, "-C", repo, "worktree", "add", worktree)
				return worktree
			},
			wantName: "r", wantOK: true,
		},
		{
			name: "the other URL form of the same remote", remote: "https://github.com/o/r",
			setOrigin: "git@github.com:o/r.git",
			dir:       func(_ *testing.T, repo string) string { return repo },
			wantName:  "r", wantOK: true,
		},
		{
			name: "no matching repo", remote: "git@github.com:o/other.git",
			dir: func(_ *testing.T, repo string) string { return repo },
		},
		{
			name: "no origin",
			dir: func(t *testing.T, _ string) string {
				t.Helper()
				return t.TempDir()
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, repoPath := repoWithOrigin(t)
			remote := tt.remote
			if remote == "" {
				remote = filepath.Join(root, "remote.git")
			}
			if tt.setOrigin != "" {
				runGit(t, "-C", repoPath, "remote", "set-url", "origin", tt.setOrigin)
			}
			repos := []store.Repo{{Name: "r", Remote: remote}}

			name, ok := loop.RepoNameForDir(t.Context(), tt.dir(t, repoPath), repos)
			if ok != tt.wantOK || name != tt.wantName {
				t.Errorf("RepoNameForDir = %q, %v, want %q, %v", name, ok, tt.wantName, tt.wantOK)
			}
		})
	}
}
