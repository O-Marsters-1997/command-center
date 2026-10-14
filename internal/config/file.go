package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/BurntSushi/toml"
)

type fileConfig struct {
	DataDir           string   `toml:"data_dir"`
	DatabaseURL       string   `toml:"database_url"`
	ClaudeProjectsDir string   `toml:"claude_projects_dir"`
	MaxAgents         int      `toml:"max_agents"`
	Port              int      `toml:"port"`
	AgentCommand      []string `toml:"agent_command"`
	MaxTurns          int      `toml:"max_turns"`
	SpendLimit5h      int      `toml:"spend_limit_5h"`
	BoardPollSeconds  int      `toml:"board_poll_seconds"`
}

func decodeFile(path string) (fileConfig, error) {
	f := fileConfig{
		Port: defaultPort, MaxAgents: defaultMaxAgents, BoardPollSeconds: DefaultBoardPollSeconds,
		AgentCommand: slices.Clone(defaultAgentCommand),
	}
	if _, err := toml.DecodeFile(path, &f); err != nil {
		return fileConfig{}, fmt.Errorf("read config %s: %w", path, err)
	}
	return f, nil
}

func resolve(f fileConfig, env Env) (Config, error) {
	dataDir, err := ResolveDataDir(f.DataDir, env)
	if err != nil {
		return Config{}, fmt.Errorf("resolve data dir: %w", err)
	}
	claudeProjectsDir, err := ResolveClaudeProjectsDir(f.ClaudeProjectsDir, env)
	if err != nil {
		return Config{}, fmt.Errorf("resolve claude projects dir: %w", err)
	}
	argv, err := agentCommandFrom(f.AgentCommand, env)
	if err != nil {
		return Config{}, fmt.Errorf("apply agent command env: %w", err)
	}
	cfg := Config{
		DataDir:           dataDir,
		DatabaseURL:       resolveDatabaseURL(f.DatabaseURL, env),
		ClaudeProjectsDir: claudeProjectsDir,
		MaxAgents:         f.MaxAgents,
		Port:              f.Port,
		AgentCommand:      argv,
		MaxTurns:          f.MaxTurns,
		SpendLimit5h:      f.SpendLimit5h,
		BoardPollSeconds:  f.BoardPollSeconds,
	}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	if cfg.MaxTurns > 0 && len(cfg.AgentCommand) > 0 {
		cfg.AgentCommand = append(slices.Clone(cfg.AgentCommand), "--max-turns", strconv.Itoa(cfg.MaxTurns))
	}
	return cfg, nil
}

const agentCommandEnv = "CC_AGENT_COMMAND"

func agentCommandFrom(configured []string, env Env) ([]string, error) {
	if env.AgentCommand == "" {
		return configured, nil
	}
	var argv []string
	if err := json.Unmarshal([]byte(env.AgentCommand), &argv); err != nil {
		return nil, fmt.Errorf("%s is not a JSON array of strings: %w", agentCommandEnv, err)
	}
	if len(argv) == 0 {
		return nil, fmt.Errorf("%s is an empty array", agentCommandEnv)
	}
	return argv, nil
}

var requiredAgentCommandParts = []string{"--permission-mode", "{system_prompt}", "{model}"}

func (c Config) validate() error {
	var errs []error
	if c.Port < 1 || c.Port > 65535 {
		errs = append(errs, fmt.Errorf("port must be 1 to 65535, got %d", c.Port))
	}
	if c.MaxAgents < 1 {
		errs = append(errs, fmt.Errorf("max_agents must be at least 1, got %d", c.MaxAgents))
	}
	if c.SpendLimit5h < 0 || c.SpendLimit5h > 100 {
		errs = append(errs, fmt.Errorf("spend_limit_5h must be 0 to 100, got %d", c.SpendLimit5h))
	}
	if c.BoardPollSeconds < 1 {
		errs = append(errs, fmt.Errorf("board_poll_seconds must be at least 1, got %d", c.BoardPollSeconds))
	}
	if c.MaxTurns < 0 {
		errs = append(errs, fmt.Errorf("max_turns must not be negative, got %d", c.MaxTurns))
	}
	if len(c.AgentCommand) > 0 {
		for _, part := range requiredAgentCommandParts {
			if !slices.Contains(c.AgentCommand, part) {
				errs = append(errs, fmt.Errorf("agent_command %q lacks %s, which every run needs", c.AgentCommand, part))
			}
		}
	}
	return errors.Join(errs...)
}
