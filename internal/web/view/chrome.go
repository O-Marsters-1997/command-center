package view

import (
	"slices"
	"strconv"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	"github.com/O-Marsters-1997/command-center/internal/plan/spend"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

type Chrome struct {
	Running        int
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
	Crumbs         []Crumb
	RepoScope      string
	FeatureScope   string
	ProjectScope   string
	ProjectHue     int
	ProjectInitial string
	ScopeMenu      []ScopeChoice
	Launch         NavItem
	Nav            []NavItem
	Sidebar        Sidebar
}

type Sidebar struct {
	Width     int
	Collapsed bool
}

const (
	sidebarMin     = 220
	sidebarMax     = 440
	sidebarInitial = 300
)

func (Sidebar) Min() int { return sidebarMin }
func (Sidebar) Max() int { return sidebarMax }

func ParseSidebar(width, collapsed string) Sidebar {
	w, err := strconv.Atoi(width)
	if err != nil {
		w = sidebarInitial
	}
	return Sidebar{Width: min(max(w, sidebarMin), sidebarMax), Collapsed: collapsed == "1"}
}

const (
	keyTickets  = "g t"
	keyInsights = "g i"
	keyRepos    = "g r"
	keyLaunch   = "c"

	appTitle = "Command Centre"
)

type Crumb struct {
	Label   string
	Path    string
	Project option.Option[ProjectMark]
	Mono    bool
}

func projectCrumb(m ProjectMark) Crumb {
	return Crumb{Label: m.Name, Path: withQuery(ticketsPath, Params{Project: m.Name}.Query()), Project: option.Some(m)}
}

func featureCrumbs(rows []Row, feature string) []Crumb {
	var crumbs []Crumb
	if i := slices.IndexFunc(rows, func(r Row) bool { return r.Project != "" }); i >= 0 {
		crumbs = append(crumbs, projectCrumb(rows[i].Mark()))
	}
	return append(crumbs, Crumb{Label: feature})
}

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
	NoReading   bool
	Stale       bool
	ReadAgo     string
}

const staleReadingAge = time.Hour

type windowSplit struct {
	Fit   spend.Result
	CCUSD float64
}

func deriveGauges(
	gauges map[agentlog.Window]store.Gauge, split map[agentlog.Window]windowSplit, now time.Time,
) []Gauge {
	return []Gauge{
		deriveGauge("five-hour", agentlog.FiveHour, gauges, split, now),
		deriveGauge("weekly", agentlog.SevenDay, gauges, split, now),
	}
}

func deriveGauge(
	label string, window agentlog.Window, gauges map[agentlog.Window]store.Gauge, split map[agentlog.Window]windowSplit,
	now time.Time,
) Gauge {
	reading, ok := gauges[window]
	gauge := Gauge{Label: label, Pct: spend.Pct(reading.Utilization), ResetsAt: reading.ResetsAt, NoReading: !ok}
	if age := now.Sub(reading.At); ok && age > staleReadingAge {
		gauge.Stale, gauge.ReadAgo = true, "read "+shortAge(age)+" ago"
	}
	w := split[window]
	gauge.CCPct, gauge.Calibrating = spend.Share(gauge.Pct, w.Fit, w.CCUSD)
	return gauge
}

type SpendPaused struct {
	Pct   int
	Limit int
}
