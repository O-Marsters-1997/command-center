package plan

import (
	"cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan/verdict"
)

const (
	SettingsFile  = ".command-centre.toml"
	TrackerGitHub = "github"
)

type RaiseMode string

const (
	RaiseAuto   RaiseMode = "auto"
	RaiseManual RaiseMode = "manual"
)

type InsightsSettings struct {
	Stages       []string `toml:"stages"`
	RollupChecks []string `toml:"rollup_checks"`
}

var DefaultRollupChecks = []string{"*-status-check"}

type RepoSettings struct {
	Tracker       string            `toml:"tracker"`
	Stacking      bool              `toml:"stacking"`
	CompatCheck   string            `toml:"compat_check"`
	MergifySHA    string            `toml:"mergify_sha"`
	Deny          []string          `toml:"deny"`
	MCP           []string          `toml:"mcp"`
	Protect       []string          `toml:"protect"`
	Checks        verdict.Predicate `toml:"checks"`
	VerifyCommand []string          `toml:"verify_command"`
	Raise         RaiseMode         `toml:"raise"`
	Insights      InsightsSettings  `toml:"insights"`
}

type Rules struct {
	Stacking     map[string]bool
	Checks       map[string]verdict.Predicate
	MergifySHA   map[string]string
	CompatCheck  map[string]string
	Verify       map[string]bool
	Raise        map[string]RaiseMode
	Workflows    map[string]Workflow
	MaxAgents    int
	MaxVerifies  int
	SpendLimit5h int
}

type Daemon struct {
	MaxAgents    int
	MaxVerifies  int
	SpendLimit5h int
	Workflows    map[string]Workflow
}

func (d Daemon) Derive(in Input) Snapshot {
	return RulesFor(d, in.Obs).Derive(in)
}

func RulesFor(daemon Daemon, obs Observation) Rules {
	n := len(obs.Settings) + len(obs.SettingsErrors)
	rules := Rules{
		Stacking:     make(map[string]bool, n),
		Checks:       make(map[string]verdict.Predicate, n),
		MergifySHA:   make(map[string]string, n),
		CompatCheck:  make(map[string]string, n),
		Verify:       make(map[string]bool, n),
		Raise:        make(map[string]RaiseMode, n),
		Workflows:    daemon.Workflows,
		MaxAgents:    daemon.MaxAgents,
		MaxVerifies:  daemon.MaxVerifies,
		SpendLimit5h: daemon.SpendLimit5h,
	}
	add := func(name string, s RepoSettings) {
		rules.Stacking[name] = s.Stacking
		rules.Checks[name] = s.Checks
		rules.MergifySHA[name] = s.MergifySHA
		rules.CompatCheck[name] = s.CompatCheck
		rules.Verify[name] = len(s.VerifyCommand) > 0
		rules.Raise[name] = cmp.Or(s.Raise, RaiseAuto)
	}
	for name := range obs.SettingsErrors {
		add(name, RepoSettings{})
	}
	for name, s := range obs.Settings {
		add(name, s)
	}
	return rules
}
