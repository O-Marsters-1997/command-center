// Package config loads the user-edited TOML file and resolves the workspace layout it names.
package config

import (
	"encoding/json"
	"fmt"
	"os"
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
	// MaxTurns caps a spawned run at this many agent turns, appended to AgentCommand as
	// --max-turns. Zero (the default) sets no cap.
	MaxTurns int `toml:"max_turns"`
	// SpendLimit5h is the percent of the account's five-hour window at or above which nothing new
	// spawns; 0 means unset.
	SpendLimit5h int `toml:"spend_limit_5h"`
	// BoardPollSeconds is how often the board refreshes itself; absent, LoadConfig defaults it to 5.
	BoardPollSeconds int `toml:"board_poll_seconds"`
	// LegacyRepos is the [[repo]] blocks, read only by the first-boot import into the repos table.
	LegacyRepos []LegacyRepo `toml:"repo"`
}

type LegacyRepo struct {
	Name   string `toml:"name"`
	Remote string `toml:"remote"`
}

const (
	defaultPort             = 7777
	defaultMaxAgents        = 1
	DefaultBoardPollSeconds = 5
)

// The model is named explicitly because the claude CLI's own default tracks Anthropic's
// latest release.
var defaultAgentCommand = []string{
	"claude", "-p", "{prompt}",
	"--output-format", "stream-json", "--verbose",
	"--settings", "{settings}",
	"--agents", "{agents}",
	"--append-system-prompt-file", "{system_prompt}",
	"--permission-mode", "auto",
	"--model", "claude-sonnet-5-5",
}

// LoadConfig decodes the config file and resolves the data directory, database and agent command.
func LoadConfig(path string) (Config, error) {
	cfg := Config{
		Port: defaultPort, MaxAgents: defaultMaxAgents, BoardPollSeconds: DefaultBoardPollSeconds,
		AgentCommand: slices.Clone(defaultAgentCommand),
	}
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return Config{}, fmt.Errorf("read config %s: %w", path, err)
	}

	if err := rejectPerRepoKeys(path); err != nil {
		return Config{}, err
	}

	if cfg.BoardPollSeconds < 1 {
		return Config{}, fmt.Errorf("config %s: board_poll_seconds must be at least 1, got %d", path, cfg.BoardPollSeconds)
	}

	dataDir, err := ResolveDataDir(cfg.DataDir)
	if err != nil {
		return Config{}, err
	}
	cfg.DataDir = dataDir
	cfg.DatabaseURL = resolveDatabaseURL(cfg.DatabaseURL)
	claudeProjectsDir, err := ResolveClaudeProjectsDir(cfg.ClaudeProjectsDir)
	if err != nil {
		return Config{}, err
	}
	cfg.ClaudeProjectsDir = claudeProjectsDir
	if err := applyAgentCommandEnv(&cfg); err != nil {
		return Config{}, err
	}
	if err := requireAgentCommandParts(cfg.AgentCommand); err != nil {
		return Config{}, err
	}
	if cfg.MaxTurns > 0 && len(cfg.AgentCommand) > 0 {
		cfg.AgentCommand = append(cfg.AgentCommand, "--max-turns", strconv.Itoa(cfg.MaxTurns))
	}
	return cfg, nil
}

const agentCommandEnv = "CC_AGENT_COMMAND"

func applyAgentCommandEnv(cfg *Config) error {
	raw := os.Getenv(agentCommandEnv)
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

var requiredAgentCommandParts = []string{"--permission-mode", "{agents}", "{system_prompt}"}

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

var perRepoKeys = []string{
	"tracker", "stacking", "deny", "checks", "compat_check", "mergify_sha", "verify_command",
}

func rejectPerRepoKeys(path string) error {
	var raw struct {
		Repos []map[string]any `toml:"repo"`
	}
	if _, err := toml.DecodeFile(path, &raw); err != nil {
		return fmt.Errorf("read config %s: %w", path, err)
	}
	for _, block := range raw.Repos {
		for _, key := range perRepoKeys {
			if _, ok := block[key]; ok {
				return fmt.Errorf("config %s: [[repo]] %v sets %q; per-repo settings now live in %s on the repo's origin/main",
					path, block["name"], key, SettingsFile)
			}
		}
	}
	return nil
}
