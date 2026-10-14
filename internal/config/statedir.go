package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Workspace is the tree under the data directory: state/ for everything the app owns and
// repos/ for the checkouts, with the worktrees tp cuts beside them.
type Workspace struct {
	// DataDir is the root the whole layout hangs off, and the name the page's header shows.
	DataDir  string
	StateDir string
	// ReposDir holds one checkout per tracked repo, at <owner>/<name>.
	ReposDir string
	// LockPath is the file the one-instance-per-workspace flock is taken on.
	LockPath string
	// RunsDir holds one <run-id>.jsonl per run: agent stdout and stderr, redirected, never
	// piped, outside any checkout so a crash never loses it.
	RunsDir string
	// SettingsPath is the app-owned deny settings file passed to every spawn.
	SettingsPath string
	// SystemPromptPath is the app-owned system prompt appended to every spawn.
	SystemPromptPath string
}

const defaultDatabaseURL = "postgres://cc:cc@localhost:5432/cc?sslmode=disable"

func resolveDatabaseURL(configured string, env Env) string {
	if configured != "" {
		return configured
	}
	if env.DatabaseURL != "" {
		return env.DatabaseURL
	}
	return defaultDatabaseURL
}

// ResolveDataDir answers where the app keeps everything: the config's own data_dir, else
// CC_DATA_DIR, else os.UserConfigDir()/command-centre. A leading ~ expands.
func ResolveDataDir(configured string, env Env) (string, error) {
	dir := configured
	if dir == "" {
		dir = env.DataDir
	}
	if dir == "" {
		if env.UserConfigDir == "" {
			return "", errors.New("resolve data dir: no data_dir, CC_DATA_DIR or user config dir")
		}
		dir = filepath.Join(env.UserConfigDir, "command-centre")
	}
	dir, err := expandHome(dir, env.Home)
	if err != nil {
		return "", err
	}
	return filepath.Abs(dir)
}

const defaultClaudeProjectsDir = "~/.claude/projects"

// ResolveClaudeProjectsDir answers where usage.Weigh reads transcripts from: the config's own
// claude_projects_dir, else defaultClaudeProjectsDir. A leading ~ expands.
func ResolveClaudeProjectsDir(configured string, env Env) (string, error) {
	dir := configured
	if dir == "" {
		dir = defaultClaudeProjectsDir
	}
	dir, err := expandHome(dir, env.Home)
	if err != nil {
		return "", err
	}
	return filepath.Abs(dir)
}

func expandHome(path, home string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	if home == "" {
		return "", fmt.Errorf("expand %s: home directory is unknown", path)
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~")), nil
}

// ResolveWorkspace lays out dataDir and creates every directory in it 0700. It derives nothing
// from where the config file happens to sit.
func ResolveWorkspace(dataDir string) (Workspace, error) {
	state := filepath.Join(dataDir, "state")
	ws := Workspace{
		DataDir:          dataDir,
		StateDir:         state,
		ReposDir:         filepath.Join(dataDir, "repos"),
		LockPath:         filepath.Join(state, "command-centre.lock"),
		RunsDir:          filepath.Join(state, "runs"),
		SettingsPath:     filepath.Join(state, "settings", "agent.json"),
		SystemPromptPath: filepath.Join(state, "settings", "system-prompt.md"),
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
