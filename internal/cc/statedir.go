package cc

import (
	"fmt"
	"os"
	"path/filepath"
)

// Workspace is the pair of directories the app works between: the workspace root the config
// file sits in and the private state dir holding the DB. The state dir sits outside the
// workspace so a DB there is never one "../" from an agent's own worktree (§8).
type Workspace struct {
	Name   string
	Root   string
	Dir    string
	DBPath string
	// LockPath is a file beside the database rather than the database itself: SQLite takes
	// its own locks on the DB, and an flock on the same file deadlocks the driver.
	LockPath string
	// RunsDir holds one <run-id>.jsonl per run: agent stdout and stderr, redirected, never
	// piped, outside the workspace so a crash never loses it.
	RunsDir string
	// SettingsPath is the app-owned deny settings file passed to every spawn (inv. 17).
	SettingsPath string
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

	runsDir := filepath.Join(dir, "runs")
	if err := os.MkdirAll(runsDir, 0o700); err != nil {
		return Workspace{}, fmt.Errorf("create runs dir %s: %w", runsDir, err)
	}
	settingsDir := filepath.Join(dir, "settings")
	if err := os.MkdirAll(settingsDir, 0o700); err != nil {
		return Workspace{}, fmt.Errorf("create settings dir %s: %w", settingsDir, err)
	}

	return Workspace{
		Name:         filepath.Base(root),
		Root:         root,
		Dir:          dir,
		DBPath:       filepath.Join(dir, "command-centre.db"),
		LockPath:     filepath.Join(dir, "command-centre.lock"),
		RunsDir:      runsDir,
		SettingsPath: filepath.Join(settingsDir, "agent.json"),
	}, nil
}
