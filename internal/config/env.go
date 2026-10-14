package config

import (
	"errors"
	"path/filepath"
	"runtime"
)

const AgentModel = "claude-sonnet-5-5"

type Env struct {
	DataDir       string
	DatabaseURL   string
	AgentCommand  string
	Home          string
	UserConfigDir string
	TZ            string
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
		AgentCommand:  lookup("CC_AGENT_COMMAND"),
		Home:          home,
		UserConfigDir: configDir,
		TZ:            lookup("TZ"),
	}, nil
}
