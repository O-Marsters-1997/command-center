package plan

import "github.com/O-Marsters-1997/command-center/internal/verdict"

const SettingsFile = ".command-centre.toml"

type RepoSettings struct {
	Tracker       string            `toml:"tracker"`
	Stacking      bool              `toml:"stacking"`
	CompatCheck   string            `toml:"compat_check"`
	MergifySHA    string            `toml:"mergify_sha"`
	Deny          []string          `toml:"deny"`
	Checks        verdict.Predicate `toml:"checks"`
	VerifyCommand []string          `toml:"verify_command"`
}

type Rules struct {
	Stacking     map[string]bool
	Checks       map[string]verdict.Predicate
	MergifySHA   map[string]string
	CompatCheck  map[string]string
	MaxAgents    int
	SpendLimit5h int
}

type Daemon struct {
	MaxAgents    int
	SpendLimit5h int
}

func RulesFor(daemon Daemon, obs Observation) Rules {
	n := len(obs.Settings) + len(obs.SettingsErrors)
	rules := Rules{
		Stacking:     make(map[string]bool, n),
		Checks:       make(map[string]verdict.Predicate, n),
		MergifySHA:   make(map[string]string, n),
		CompatCheck:  make(map[string]string, n),
		MaxAgents:    daemon.MaxAgents,
		SpendLimit5h: daemon.SpendLimit5h,
	}
	add := func(name string, s RepoSettings) {
		rules.Stacking[name] = s.Stacking
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
