package cc

import (
	"fmt"
	"os"
	"path/filepath"
)

// Workspace is the pair of directories the app works between: the workspace root the config
// file sits in (repo paths are relative to it) and the private state dir holding the DB.
//
// The state dir is deliberately outside the workspace: worktrees are siblings of the config
// dir, so a DB under it would be one "../" from every agent (§8).
type Workspace struct {
	Name   string
	Root   string
	Dir    string
	DBPath string
	// LockPath is a file beside the database rather than the database itself: SQLite takes
	// its own locks on the DB, and an flock on the same file deadlocks the driver.
	LockPath string
}

// ResolveWorkspace derives the workspace from the config path and creates its state dir 0700.
// The workspace root is the config file's parent directory's parent — plain/.claude/x.toml
// is the workspace "plain".
func ResolveWorkspace(configPath string) (Workspace, error) {
	abs, err := filepath.Abs(configPath)
	if err != nil {
		return Workspace{}, fmt.Errorf("resolve config path %s: %w", configPath, err)
	}
	root := filepath.Dir(filepath.Dir(abs))

	base, err := os.UserConfigDir()
	if err != nil {
		return Workspace{}, fmt.Errorf("resolve user config dir: %w", err)
	}
	dir := filepath.Join(base, "command-centre", filepath.Base(root))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Workspace{}, fmt.Errorf("create state dir %s: %w", dir, err)
	}

	return Workspace{
		Name:     filepath.Base(root),
		Root:     root,
		Dir:      dir,
		DBPath:   filepath.Join(dir, "command-centre.db"),
		LockPath: filepath.Join(dir, "command-centre.lock"),
	}, nil
}
