package view

import (
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/spend"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

type Chrome struct {
	Workspace     string
	LiveAgents    int
	Observe       Age
	ObserveStale  bool
	LastError     *TickError
	Gauges        []Gauge
	SpendPaused   *SpendPaused
	RefusedRepos  []RefusedRepo
	Section       string
	Home          bool
	RepoScope     string
	RepoCrumb     string
	RepoCrumbPath string
	FeatureScope  string
	FeatureQuery  string
}

// Age is a relative time the server renders and the page's clock keeps current. Stamp is the
// instant the browser counts from, and is empty when there is none to count from.
type Age struct {
	Age   string
	Stamp string
}

// RefusedRepo is a tracked repo the loop refused to work, linked to its own page.
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

func deriveGauges(gauges map[agentlog.Window]store.Gauge, split map[agentlog.Window]windowSplit) []Gauge {
	return []Gauge{
		deriveGauge("five-hour", agentlog.FiveHour, gauges, split),
		deriveGauge("weekly", agentlog.SevenDay, gauges, split),
	}
}

func deriveGauge(
	label string, window agentlog.Window, gauges map[agentlog.Window]store.Gauge, split map[agentlog.Window]windowSplit,
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
