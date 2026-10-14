package app_test

import (
	"path/filepath"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/gittest"
)

func repoWithOrigin(t *testing.T) (root, repoPath string) {
	t.Helper()
	repoPath, origin := gittest.WithOrigin(t)
	return filepath.Dir(origin), repoPath
}
