// Package cc is the Command Centre's imperative shell: config, state dir, store, loop and page.
// The pure decisions live in internal/plan; gh's JSON shape lives in internal/gh.
package cc

import (
	"fmt"

	"github.com/BurntSushi/toml"
)

// Config is the user-edited TOML file named by --config. See docs/command-centre-v1.md §8.
type Config struct {
	MaxAgents    int      `toml:"max_agents"`
	Port         int      `toml:"port"`
	AgentCommand []string `toml:"agent_command"`
	Tasks        []Task   `toml:"task"`
	Repos        []Repo   `toml:"repo"`
}

// Task is one [[task]] block: Phase 1 intake, upserted on TicketURL at startup.
type Task struct {
	TicketURL string   `toml:"ticket_url"`
	Repo      string   `toml:"repo"`
	Branch    string   `toml:"branch"`
	BlockedBy []string `toml:"blocked_by"`
}

// Repo is one [[repo]] block. Path is relative to the workspace root.
type Repo struct {
	Name        string   `toml:"name"`
	Path        string   `toml:"path"`
	Stacking    bool     `toml:"stacking"`
	CompatCheck string   `toml:"compat_check"`
	MergifySHA  string   `toml:"mergify_sha"`
	Deny        []string `toml:"deny"`
}

const (
	defaultPort      = 7777
	defaultMaxAgents = 1
)

// LoadConfig decodes the config file and rejects a task whose repo has no [[repo]] block.
func LoadConfig(path string) (Config, error) {
	cfg := Config{Port: defaultPort, MaxAgents: defaultMaxAgents}
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return Config{}, fmt.Errorf("read config %s: %w", path, err)
	}

	byName := make(map[string]bool, len(cfg.Repos))
	for _, r := range cfg.Repos {
		byName[r.Name] = true
	}
	for _, t := range cfg.Tasks {
		if !byName[t.Repo] {
			return Config{}, fmt.Errorf("task %s names repo %q with no [[repo]] block", t.TicketURL, t.Repo)
		}
	}
	return cfg, nil
}
