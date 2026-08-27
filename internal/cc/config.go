// Package cc is the Command Centre's imperative shell: config, state dir, store, loop and page.
// The pure decisions live in internal/plan; gh's JSON shape lives in internal/gh.
package cc

import (
	"fmt"
	"path/filepath"
	"slices"

	"github.com/BurntSushi/toml"

	"github.com/O-Marsters-1997/command-center/internal/verdict"
)

// Config is the user-edited TOML file named by --config. See docs/designs/command-centre-design.md §8.
type Config struct {
	// DataDir holds the resolved data directory after LoadConfig, not the raw config key.
	DataDir      string   `toml:"data_dir"`
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

// Repo is one [[repo]] block. A repo is located by Remote, a git URL the app clones, or by
// Path, an existing checkout. Exactly one of the two.
// Checks, MergifySHA and CompatCheck are all empty until a repo opts into a CI verdict, matching
// the pre-Phase-5 behaviour where every row stops at checking (docs/designs/command-centre-design.md § 11 inv. 11).
type Repo struct {
	Name        string            `toml:"name"`
	Remote      string            `toml:"remote"`
	Path        string            `toml:"path"`
	Stacking    bool              `toml:"stacking"`
	CompatCheck string            `toml:"compat_check"`
	MergifySHA  string            `toml:"mergify_sha"`
	Deny        []string          `toml:"deny"`
	Checks      verdict.Predicate `toml:"checks"`
	// Checkout is where this repo's working copy is, resolved once by LoadConfig. Everything
	// downstream reads this and derives no path of its own. Not a config key.
	Checkout string `toml:"-"`
}

const (
	defaultPort      = 7777
	defaultMaxAgents = 1
)

// defaultAgentCommand is the argv a config naming no agent_command gets. The model is named
// explicitly because the CLI's own default tracks Anthropic's latest release, so leaving it off
// would change what every run is built by without this repo changing (§8).
var defaultAgentCommand = []string{
	"claude", "-p", "{prompt}", "--settings", "{settings}", "--model", "claude-sonnet-5",
}

// LoadConfig decodes the config file, resolves the data directory and each repo's checkout, and
// rejects a task whose repo has no [[repo]] block. Where the config file sits decides one thing
// only: what a relative repo path is relative to.
func LoadConfig(path string) (Config, error) {
	cfg := Config{Port: defaultPort, MaxAgents: defaultMaxAgents, AgentCommand: slices.Clone(defaultAgentCommand)}
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return Config{}, fmt.Errorf("read config %s: %w", path, err)
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
	for i, r := range cfg.Repos {
		checkout, err := r.CheckoutPath(dataDir, configDir)
		if err != nil {
			return Config{}, err
		}
		cfg.Repos[i].Checkout = checkout
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

// stackingByRepo indexes each configured repo's stacking flag by name — consulted on every
// unlock decision, in both the page and the loop's own launch-eligibility check.
func stackingByRepo(repos []Repo) map[string]bool {
	m := make(map[string]bool, len(repos))
	for _, r := range repos {
		m[r.Name] = r.Stacking
	}
	return m
}

// denyByRepo indexes each configured repo's per-repo push-policy additions by name -- the push
// step's own per-repo half of plan.Policy (the default set lives in internal/plan).
func denyByRepo(repos []Repo) map[string][]string {
	m := make(map[string][]string, len(repos))
	for _, r := range repos {
		m[r.Name] = r.Deny
	}
	return m
}

// checksByRepo indexes each configured repo's CI verdict predicate by name -- internal/verdict's
// own input, read fresh at render time (inv. 14).
func checksByRepo(repos []Repo) map[string]verdict.Predicate {
	m := make(map[string]verdict.Predicate, len(repos))
	for _, r := range repos {
		m[r.Name] = r.Checks
	}
	return m
}

// mergifySHAByRepo indexes each configured repo's recorded .mergify.yml hash by name -- the
// value the predicate was written against, compared each tick to the file's current hash
// (docs/designs/command-centre-design.md § 7).
func mergifySHAByRepo(repos []Repo) map[string]string {
	m := make(map[string]string, len(repos))
	for _, r := range repos {
		m[r.Name] = r.MergifySHA
	}
	return m
}

// compatCheckByRepo indexes each configured repo's cross-repo compat check name by name --
// internal/verdict's inv. 12 input, empty for a repo that never opted in
// (docs/designs/command-centre-design.md § 11 inv. 12).
func compatCheckByRepo(repos []Repo) map[string]string {
	m := make(map[string]string, len(repos))
	for _, r := range repos {
		m[r.Name] = r.CompatCheck
	}
	return m
}

func repoPathsByName(repos []Repo) map[string]string {
	m := make(map[string]string, len(repos))
	for _, r := range repos {
		m[r.Name] = r.Checkout
	}
	return m
}
