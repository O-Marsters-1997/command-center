// Package config loads the user-edited TOML file and resolves the workspace layout it names.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/tracker"
	"github.com/O-Marsters-1997/command-center/internal/verdict"
)

// Config is the user-edited TOML file named by --config. See docs/designs/command-centre-design.md §8.
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
	// SpendLimit5h is the percent of the account's five-hour window at or above which
	// launchEligible spawns nothing new (CC-314); 0 means unset, so nothing is ever paused.
	SpendLimit5h int `toml:"spend_limit_5h"`
	// BoardPollSeconds is how often the board refreshes itself; absent, LoadConfig defaults it to 5.
	BoardPollSeconds int    `toml:"board_poll_seconds"`
	Repos            []Repo `toml:"repo"`
}

// Repo is one [[repo]] block. A repo is located by Remote, a git URL the app clones, or by
// Path, an existing checkout. Exactly one of the two.
// Checks, MergifySHA and CompatCheck are all empty until a repo opts into a CI verdict, matching
// the pre-Phase-5 behaviour where every row stops at checking (docs/designs/command-centre-design.md § 11 inv. 11).
type Repo struct {
	Name   string `toml:"name"`
	Remote string `toml:"remote"`
	// Tracker names which issue tracker this repo's tickets live in. Absent, LoadConfig defaults
	// it to "github".
	Tracker     string            `toml:"tracker"`
	Path        string            `toml:"path"`
	Stacking    bool              `toml:"stacking"`
	CompatCheck string            `toml:"compat_check"`
	MergifySHA  string            `toml:"mergify_sha"`
	Deny        []string          `toml:"deny"`
	Checks      verdict.Predicate `toml:"checks"`
	// VerifyCommand is the argv a clean refresh or restack is verified with before the row is
	// offered as sound (issue #110); empty means the repo opted out.
	VerifyCommand []string `toml:"verify_command"`
	// Generated names the paths this repo's build regenerates, glob-matched against a conflict
	// with origin/main; empty means the repo opted out.
	Generated []string `toml:"generated"`
	// BuildCommand is the argv that regenerates Generated's paths, run in the ticket's own
	// worktree before they are staged and committed; empty means the repo opted out.
	BuildCommand []string `toml:"build_command"`
	// Checkout is where this repo's working copy is, resolved once by LoadConfig. Everything
	// downstream reads this and derives no path of its own. Not a config key.
	Checkout string `toml:"-"`
}

const (
	defaultPort             = 7777
	defaultMaxAgents        = 1
	DefaultBoardPollSeconds = 5
)

// defaultAgentCommand is the argv a config naming no agent_command gets. The model is named
// explicitly because the CLI's own default tracks Anthropic's latest release, so leaving it off
// would change what every run is built by without this repo changing (§8).
var defaultAgentCommand = []string{
	"claude", "-p", "{prompt}",
	"--output-format", "stream-json", "--verbose",
	"--settings", "{settings}",
	"--agents", "{agents}",
	"--append-system-prompt-file", "{system_prompt}",
	"--permission-mode", "auto",
	"--model", "claude-sonnet-5-5",
}

// LoadConfig decodes the config file, resolves the data directory and each repo's checkout, and
// rejects a ticket whose repo has no [[repo]] block. Where the config file sits decides one thing
// only: what a relative repo path is relative to.
func LoadConfig(path string) (Config, error) {
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

	configDir, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return Config{}, fmt.Errorf("resolve config path %s: %w", path, err)
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
	for i, r := range cfg.Repos {
		if r.Tracker == "" {
			cfg.Repos[i].Tracker = string(tracker.GitHub)
		}
		checkout, err := r.CheckoutPath(dataDir, configDir)
		if err != nil {
			return Config{}, err
		}
		cfg.Repos[i].Checkout = checkout
	}
	return cfg, nil
}

// agentCommandEnv replaces agent_command with a machine-local wrapper, as a JSON array.
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

// PlanRules builds the rules every decision reads, indexing each configured repo's settings by
// name. It is the only place those per-repo maps are built.
func (c Config) PlanRules() plan.Rules {
	rules := plan.Rules{
		Stacking:     make(map[string]bool, len(c.Repos)),
		Deny:         make(map[string][]string, len(c.Repos)),
		Checks:       make(map[string]verdict.Predicate, len(c.Repos)),
		MergifySHA:   make(map[string]string, len(c.Repos)),
		CompatCheck:  make(map[string]string, len(c.Repos)),
		MaxAgents:    c.MaxAgents,
		SpendLimit5h: c.SpendLimit5h,
	}
	for _, r := range c.Repos {
		rules.Stacking[r.Name] = r.Stacking
		rules.Deny[r.Name] = r.Deny
		rules.Checks[r.Name] = r.Checks
		rules.MergifySHA[r.Name] = r.MergifySHA
		rules.CompatCheck[r.Name] = r.CompatCheck
	}
	return rules
}

// CheckoutPath answers where a repo's working copy is. A remote repo's checkout is one the app
// makes and names, at <dataDir>/repos/<name>; a path repo's is one the operator made, absolute
// or relative to configDir. Exactly one of the two forms is allowed.
func (r Repo) CheckoutPath(dataDir, configDir string) (string, error) {
	switch {
	case r.Remote != "" && r.Path != "":
		return "", fmt.Errorf("repo %s sets both remote and path: pick one", r.Name)
	case r.Remote == "" && r.Path == "":
		return "", fmt.Errorf("repo %s sets neither remote nor path", r.Name)
	case r.Remote != "":
		if err := validRepoName(r.Name); err != nil {
			return "", err
		}
		return filepath.Join(dataDir, "repos", r.Name), nil
	}

	path, err := expandHome(r.Path)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(configDir, path)
	}
	return filepath.Clean(path), nil
}

func validRepoName(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("repo name %q is not a single directory name", name)
	}
	return nil
}
