// Package config loads the user-edited TOML file and resolves the workspace layout it names.
package config

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/BurntSushi/toml"
)

// Config is the user-edited TOML file named by --config.
type Config struct {
	// DataDir holds the resolved data directory after LoadConfig, not the raw config key.
	DataDir string `toml:"data_dir"`
	// DatabaseURL holds the resolved connection string after LoadConfig, not the raw config key.
	DatabaseURL string `toml:"database_url"`
	// ClaudeProjectsDir holds the resolved directory after LoadConfig, not the raw config key.
	ClaudeProjectsDir string   `toml:"claude_projects_dir"`
	MaxAgents         int      `toml:"max_agents"`
	Port              int      `toml:"port"`
	AgentCommand      []string `toml:"agent_command"`
	MaxTurns          int      `toml:"max_turns"`
	// SpendLimit5h is the percent of the account's five-hour window at or above which nothing new
	// spawns; 0 means unset.
	SpendLimit5h int `toml:"spend_limit_5h"`
	// BoardPollSeconds is how often the board refreshes itself; absent, LoadConfig defaults it to 5.
	BoardPollSeconds int `toml:"board_poll_seconds"`
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

// LoadConfig decodes the config file and resolves the data directory, database and agent command.
func LoadConfig(path string, env Env) (Config, error) {
	cfg := Config{
		Port: defaultPort, MaxAgents: defaultMaxAgents, BoardPollSeconds: DefaultBoardPollSeconds,
		AgentCommand: slices.Clone(defaultAgentCommand),
	}
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return Config{}, fmt.Errorf("read config %s: %w", path, err)
	}

	if cfg.BoardPollSeconds < 1 {
		return Config{}, fmt.Errorf("config %s: board_poll_seconds must be at least 1, got %d", path, cfg.BoardPollSeconds)
	}

	dataDir, err := ResolveDataDir(cfg.DataDir, env)
	if err != nil {
		return Config{}, err
	}
	cfg.DataDir = dataDir
	cfg.DatabaseURL = resolveDatabaseURL(cfg.DatabaseURL, env)
	claudeProjectsDir, err := ResolveClaudeProjectsDir(cfg.ClaudeProjectsDir, env)
	if err != nil {
		return Config{}, err
	}
	cfg.ClaudeProjectsDir = claudeProjectsDir
	if err := applyAgentCommandEnv(&cfg, env); err != nil {
		return Config{}, err
	}
	if err := requireAgentCommandParts(cfg.AgentCommand); err != nil {
		return Config{}, err
	}
	if cfg.MaxTurns > 0 && len(cfg.AgentCommand) > 0 {
		cfg.AgentCommand = withMaxTurns(cfg.AgentCommand, cfg.MaxTurns)
	}
	return cfg, nil
}

func withMaxTurns(argv []string, turns int) []string {
	return append(slices.Clone(argv), "--max-turns", strconv.Itoa(turns))
}

const agentCommandEnv = "CC_AGENT_COMMAND"

func applyAgentCommandEnv(cfg *Config, env Env) error {
	raw := env.AgentCommand
	if raw == "" {
		return nil
	}
	var argv []string
	if err := json.Unmarshal([]byte(raw), &argv); err != nil {
		return fmt.Errorf("%s is not a JSON array of strings: %w", agentCommandEnv, err)
	}
	if len(argv) == 0 {
		return fmt.Errorf("%s is an empty array", agentCommandEnv)
	}
	cfg.AgentCommand = argv
	return nil
}

var requiredAgentCommandParts = []string{"--permission-mode", "{system_prompt}", "{model}"}

func requireAgentCommandParts(argv []string) error {
	if len(argv) == 0 {
		return nil
	}
	for _, part := range requiredAgentCommandParts {
		if !slices.Contains(argv, part) {
			return fmt.Errorf("agent_command %q lacks %s, which every run needs", argv, part)
		}
	}
	return nil
}

// CheckoutPath is where the app keeps the working copy of the repo named owner/name.
func CheckoutPath(dataDir, fullName string) string {
	return filepath.Join(dataDir, "repos", filepath.FromSlash(fullName))
}
