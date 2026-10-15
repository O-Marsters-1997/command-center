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
	DataDir          string
	StateDir         string
	ReposDir         string
	LockPath         string
	RunsDir          string
	SettingsPath     string
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
		return "", fmt.Errorf("expand data dir: %w", err)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("absolute data dir %s: %w", dir, err)
	}
	return abs, nil
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
		return "", fmt.Errorf("expand claude projects dir: %w", err)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("absolute claude projects dir %s: %w", dir, err)
	}
	return abs, nil
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

// ResolveWorkspace creates the workspace directories under dataDir with mode 0700.
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
