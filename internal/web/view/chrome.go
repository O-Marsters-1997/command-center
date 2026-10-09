package view

import (
	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/spend"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

// Chrome is the shell every page wears: workspace, the live and observe pills, the last tick's
// error, the current board/graph view, and the repo/feature scope links. layout.tmpl's topbar and
// masthead render it once per page load; every page's view model embeds it.
type Chrome struct {
	Workspace         string
	LiveAgents        int
	Exploring         int
	Observe           Age
	ObserveStale      bool
	LastError         *TickError
	Gauges            []Gauge
	SpendPaused       *SpendPaused
	RefusedRepos      []RefusedRepo
	View              string
	Section           string
	Home              bool
	RepoScope         string
	RepoCrumb         string
	RepoCrumbPath     string
	FeatureScope      string
	FeatureImportPath string
	FeatureQuery      string
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

// Gauge is one window's masthead gauge. Calibrating is true below spend.MinSamples trailing
// intervals, when CCPct has no meaning.
type Gauge struct {
	Label       string
	Pct         int
	Calibrating bool
	CCPct       int
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
	view := Gauge{Label: label, Pct: spend.Pct(gauges[window].Utilization)}
	w := split[window]
	view.CCPct, view.Calibrating = spend.Share(view.Pct, w.Fit, w.CCUSD)
	return view
}

type SpendPaused struct {
	Pct   int
	Limit int
}
