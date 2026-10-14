// Package config loads the user-edited TOML file and resolves the workspace layout it names.
package config

import (
	"fmt"
	"path/filepath"
)

type Config struct {
	DataDir           string
	DatabaseURL       string
	ClaudeProjectsDir string
	MaxAgents         int
	Port              int
	AgentCommand      []string
	MaxTurns          int
	// SpendLimit5h is the percent of the account's five-hour window at or above which nothing new
	// spawns; 0 means unset.
	SpendLimit5h     int
	BoardPollSeconds int
}

const (
	defaultPort             = 7777
	defaultMaxAgents        = 1
	DefaultBoardPollSeconds = 5
)

var defaultAgentCommand = []string{
	"claude", "-p", "{prompt}",
	"--output-format", "stream-json", "--verbose",
	"--settings", "{settings}",
	"--append-system-prompt-file", "{system_prompt}",
	"--permission-mode", "auto",
	"--model", "{model}",
	"{session}",
}

func Load(path string, env Env) (Config, error) {
	f, err := decodeFile(path)
	if err != nil {
		return Config{}, err
	}
	cfg, err := resolve(f, env)
	if err != nil {
		return Config{}, fmt.Errorf("config %s: %w", path, err)
	}
	return cfg, nil
}

// CheckoutPath is where the app keeps the working copy of the repo named owner/name.
func CheckoutPath(dataDir, fullName string) string {
	return filepath.Join(dataDir, "repos", filepath.FromSlash(fullName))
}
