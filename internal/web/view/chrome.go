package view

import (
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/option"
	"github.com/O-Marsters-1997/command-center/internal/spend"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

type Chrome struct {
	Workspace      string
	LiveAgents     int
	Observe        Age
	ObserveStale   bool
	LastError      option.Option[TickError]
	Gauges         []Gauge
	SpendPaused    option.Option[SpendPaused]
	RefusedRepos   []RefusedRepo
	OpenTickets    int
	PageTitle      string
	Home           bool
	Keymap         []KeyGroup
	RepoScope      string
	RepoCrumb      string
	RepoCrumbPath  string
	FeatureScope   string
	ProjectScope   string
	ProjectHue     int
	ProjectInitial string
	ScopeMenu      []ScopeChoice
	Launch         NavItem
	Nav            []NavItem
}

const (
	keyTickets  = "g t"
	keyInsights = "g i"
	keyRepos    = "g r"
	keyLaunch   = "c"

	appTitle = "Command Centre"
)

type NavItem struct {
	Label   string
	Path    string
	Key     string
	Icon    string
	Badge   string
	Warn    bool
	Current bool
}

func (c *Chrome) setCurrent(key string) {
	c.Launch.Current = c.Launch.Key == key
	for i := range c.Nav {
		c.Nav[i].Current = c.Nav[i].Key == key
	}
}

func (c *Chrome) badge(key, text string, warn bool) {
	for i := range c.Nav {
		if c.Nav[i].Key == key {
			c.Nav[i].Badge, c.Nav[i].Warn = text, warn
		}
	}
}

func (c *Chrome) titled(page string) { c.PageTitle = page + " · " + appTitle }

func (c *Chrome) scopeToProject(p store.Project) {
	c.ProjectScope, c.ProjectHue = p.Name, p.Hue
	c.ProjectInitial = initial(p.Name)
}

type Age struct {
	Age   string
	Stamp string
}

type RefusedRepo struct {
	Name   string
	Reason string
	Path   string
}

type TickError struct {
	Age     Age
	Message string
}

type Gauge struct {
	Label       string
	Pct         int
	Calibrating bool
	CCPct       int
	ResetsAt    time.Time
}

type windowSplit struct {
	Fit   spend.Result
	CCUSD float64
}

func deriveGauges(gauges map[agentlog.Window]agentlog.Reading, split map[agentlog.Window]windowSplit) []Gauge {
	return []Gauge{
		deriveGauge("five-hour", agentlog.FiveHour, gauges, split),
		deriveGauge("weekly", agentlog.SevenDay, gauges, split),
	}
}

func deriveGauge(
	label string, window agentlog.Window,
	gauges map[agentlog.Window]agentlog.Reading, split map[agentlog.Window]windowSplit,
) Gauge {
	view := Gauge{Label: label, Pct: spend.Pct(gauges[window].Utilization), ResetsAt: gauges[window].ResetsAt}
	w := split[window]
	view.CCPct, view.Calibrating = spend.Share(view.Pct, w.Fit, w.CCUSD)
	return view
}

type SpendPaused struct {
	Pct   int
	Limit int
}
