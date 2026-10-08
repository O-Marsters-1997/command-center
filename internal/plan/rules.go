package plan

import "github.com/O-Marsters-1997/command-center/internal/verdict"

// SettingsFile is the repo-root file holding one repo's settings, read from origin/main.
const SettingsFile = ".command-centre.toml"

// RepoSettings is everything a target repo says about itself in SettingsFile. Checks, MergifySHA
// and CompatCheck are empty until the repo opts into a CI verdict.
type RepoSettings struct {
	// Tracker names which issue tracker this repo's tickets live in; absent, it is "github".
	Tracker     string            `toml:"tracker"`
	Stacking    bool              `toml:"stacking"`
	CompatCheck string            `toml:"compat_check"`
	MergifySHA  string            `toml:"mergify_sha"`
	Deny        []string          `toml:"deny"`
	Checks      verdict.Predicate `toml:"checks"`
	// VerifyCommand is the argv a clean refresh or restack is verified with; empty means opted out.
	VerifyCommand []string `toml:"verify_command"`
}

type Rules struct {
	Stacking     map[string]bool
	Deny         map[string][]string
	Checks       map[string]verdict.Predicate
	MergifySHA   map[string]string
	CompatCheck  map[string]string
	MaxAgents    int
	SpendLimit5h int
}

// Daemon is the config keys that belong to the app rather than to any one repo.
type Daemon struct {
	MaxAgents    int
	SpendLimit5h int
}

// RulesFor builds the rules every decision reads from the daemon's own keys plus the settings
// this observation read from each repo. A repo whose settings failed to read still gets an entry,
// so scope checks see it. It is the only place the per-repo maps are built.
func RulesFor(daemon Daemon, obs Observation) Rules {
	n := len(obs.Settings) + len(obs.SettingsErrors)
	rules := Rules{
		Stacking:     make(map[string]bool, n),
		Deny:         make(map[string][]string, n),
		Checks:       make(map[string]verdict.Predicate, n),
		MergifySHA:   make(map[string]string, n),
		CompatCheck:  make(map[string]string, n),
		MaxAgents:    daemon.MaxAgents,
		SpendLimit5h: daemon.SpendLimit5h,
	}
	add := func(name string, s RepoSettings) {
		rules.Stacking[name] = s.Stacking
		rules.Deny[name] = s.Deny
		rules.Checks[name] = s.Checks
		rules.MergifySHA[name] = s.MergifySHA
		rules.CompatCheck[name] = s.CompatCheck
	}
	for name := range obs.SettingsErrors {
		add(name, RepoSettings{})
	}
	for name, s := range obs.Settings {
		add(name, s)
	}
	return rules
}
