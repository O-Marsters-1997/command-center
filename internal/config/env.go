package config

import (
	"errors"
	"path/filepath"
	"runtime"
)

const AgentModel = "claude-sonnet-5-5"
const agentCommandEnv = "CC_AGENT_COMMAND"

type Env struct {
	DataDir       string
	DatabaseURL   string
	AgentCommand  string
	Home          string
	UserConfigDir string
}

func EnvFrom(lookup func(string) string) (Env, error) {
	home := lookup("HOME")
	if home == "" {
		return Env{}, errors.New("HOME is not set")
	}
	configDir := lookup("XDG_CONFIG_HOME")
	switch {
	case configDir != "":
	case runtime.GOOS == "darwin":
		configDir = filepath.Join(home, "Library", "Application Support")
	default:
		configDir = filepath.Join(home, ".config")
	}
	return Env{
		DataDir:       lookup("CC_DATA_DIR"),
		DatabaseURL:   lookup("CC_DATABASE_URL"),
		AgentCommand:  lookup(agentCommandEnv),
		Home:          home,
		UserConfigDir: configDir,
	}, nil
}
