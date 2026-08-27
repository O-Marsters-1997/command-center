package cc

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Workspace is the tree under the data directory: state/ for everything the app owns and
// repos/ for the checkouts, with the worktrees tp cuts beside them. The two are siblings so
// a database is never one "../" from an agent's own worktree (§8).
type Workspace struct {
	StateDir string
	// ReposDir holds one checkout per configured repo, named after the repo.
	ReposDir string
	DBPath   string
	// LockPath is a file beside the database rather than the database itself: SQLite takes
	// its own locks on the DB, and an flock on the same file deadlocks the driver.
	LockPath string
	// RunsDir holds one <run-id>.jsonl per run: agent stdout and stderr, redirected, never
	// piped, outside any checkout so a crash never loses it.
	RunsDir string
	// SettingsPath is the app-owned deny settings file passed to every spawn (inv. 17).
	SettingsPath string
}

// dataDirEnv names the data directory when the config file does not.
const dataDirEnv = "CC_DATA_DIR"

// ResolveDataDir answers where the app keeps everything: the config's own data_dir, else
// CC_DATA_DIR, else os.UserConfigDir()/command-centre. A leading ~ expands.
func ResolveDataDir(configured string) (string, error) {
	dir := configured
	if dir == "" {
		dir = os.Getenv(dataDirEnv)
	}
	if dir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("resolve user config dir: %w", err)
		}
		dir = filepath.Join(base, "command-centre")
	}
	dir, err := expandHome(dir)
	if err != nil {
		return "", err
	}
	return filepath.Abs(dir)
}

func expandHome(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("expand %s: %w", path, err)
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~")), nil
}

// ResolveWorkspace lays out dataDir and creates every directory in it 0700. It derives nothing
// from where the config file happens to sit.
func ResolveWorkspace(dataDir string) (Workspace, error) {
	state := filepath.Join(dataDir, "state")
	ws := Workspace{
		StateDir:     state,
		ReposDir:     filepath.Join(dataDir, "repos"),
		DBPath:       filepath.Join(state, "command-centre.db"),
		LockPath:     filepath.Join(state, "command-centre.lock"),
		RunsDir:      filepath.Join(state, "runs"),
		SettingsPath: filepath.Join(state, "settings", "agent.json"),
	}
	for _, dir := range []string{
		ws.StateDir, ws.ReposDir, ws.RunsDir, filepath.Dir(ws.SettingsPath),
	} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return Workspace{}, fmt.Errorf("create %s: %w", dir, err)
		}
	}
	return ws, nil
}
