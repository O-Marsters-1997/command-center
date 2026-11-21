package config

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Workspace is the tree under the data directory: state/ for everything the app owns and
// repos/ for the checkouts, with the worktrees treepad cuts beside them.
type Workspace struct {
	DataDir          string
	StateDir         string
	ReposDir         string
	LockPath         string
	RunsDir          string
	SettingsPath     string
	SystemPromptPath string
}

const (
	defaultDatabaseURL       = "postgres://cc:cc@localhost:5432/cc?sslmode=disable"
	defaultClaudeProjectsDir = "~/.claude/projects"
	reposDirName             = "repos"
)

func resolveDatabaseURL(configured string, env Env) string {
	return cmp.Or(configured, env.DatabaseURL, defaultDatabaseURL)
}

// ResolveDataDir answers where the app keeps everything: the config's own data_dir, else
// CC_DATA_DIR, else os.UserConfigDir()/command-centre. A leading ~ expands.
func ResolveDataDir(configured string, env Env) (string, error) {
	dir := cmp.Or(configured, env.DataDir)
	if dir == "" {
		if env.UserConfigDir == "" {
			return "", errors.New("resolve data dir: no data_dir, CC_DATA_DIR or user config dir")
		}
		dir = filepath.Join(env.UserConfigDir, "command-centre")
	}
	return absHome("data dir", dir, env.Home)
}

// ResolveClaudeProjectsDir answers where usage.Weigh reads transcripts from: the config's own
// claude_projects_dir, else defaultClaudeProjectsDir. A leading ~ expands.
func ResolveClaudeProjectsDir(configured string, env Env) (string, error) {
	return absHome("claude projects dir", cmp.Or(configured, defaultClaudeProjectsDir), env.Home)
}

func absHome(what, dir, home string) (string, error) {
	dir, err := expandHome(dir, home)
	if err != nil {
		return "", fmt.Errorf("expand %s: %w", what, err)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("absolute %s %s: %w", what, dir, err)
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
		ReposDir:         filepath.Join(dataDir, reposDirName),
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
