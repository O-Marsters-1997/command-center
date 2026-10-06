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
	Workspace  string
	LiveAgents int
	Observe    Age
	// ObserveStale is decided here rather than in the template, which cannot compare durations.
	ObserveStale bool
	LastError    *TickError
	// Gauges is the masthead's own read of account utilization, five-hour then weekly, always both
	// (CC-310), each split into cc's own share and other use once its fit has enough samples
	// (CC-313).
	Gauges []Gauge
	// SpendPaused names spend_limit_5h as the reason launchEligible spawned nothing this tick, nil
	// whenever the five-hour reading is below the limit or no limit is configured (CC-314).
	SpendPaused *SpendPaused
	// View picks which of board and graph page.tmpl shows; parseViewParams defaults it to board.
	View string
	// Section names the sidebar's current destination: "board", "graph" or "features". It tracks
	// View except on /features, which has no ?view= of its own.
	Section string
	// RepoScope is this render's normalised ?repo= value, empty when unscoped. The board's own
	// Row template reads it to name a kept group's out-of-scope member (CONTEXT.md § Scope).
	RepoScope string
	// RepoLinks is the breadcrumb's own repo switcher (CONTEXT.md § Scope), empty when no repo is
	// configured so the breadcrumb renders no switcher at all.
	RepoLinks []ScopeLink
	// FeatureScope is this render's normalised ?feature= value, empty when unscoped. The board's
	// own row template reads it to name a kept group's out-of-scope member (CONTEXT.md § Feature).
	FeatureScope string
	// FeatureImportPath is the breadcrumb's reimport action, set only when FeatureScope names one
	// feature to reimport.
	FeatureImportPath string
	// FeatureQuery is FeatureScope, url.QueryEscape'd for the sidebar's board/graph links to carry
	// the scope across views; empty whenever FeatureScope is.
	FeatureQuery string
}

// ScopeLink is one breadcrumb switcher entry: "all" plus one per configured repo.
type ScopeLink struct {
	Name    string
	Path    string
	Current bool
}

// Age is a relative time the server renders and the page's clock keeps current. Stamp is the
// instant the browser counts from, and is empty when there is none to count from.
type Age struct {
	Age   string
	Stamp string
}

type TickError struct {
	Age     Age
	Message string
}

// Gauge is one window's masthead gauge: a fixed label so the DOM shape never changes between
// polls, and the meter's own fill percentage, 0 for a window with no reading yet. Calibrating is
// true below spend.MinSamples trailing intervals, when CCPct -- cc's own share -- has no meaning.
type Gauge struct {
	Label       string
	Pct         int
	Calibrating bool
	CCPct       int
}

// windowSplit is one window's cc-vs-other input: the fit and cc's own recorded cost_usd within
// that window's trailing span, fetched once per render by Server.gaugeSplit.
type windowSplit struct {
	Fit   spend.Result
	CCUSD float64
}

// deriveGauges always returns the five-hour and weekly gauges in that fixed order, whether or not
// either window has a reading yet (CC-310).
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
