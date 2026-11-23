package view

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/plan/insight"
	"github.com/O-Marsters-1997/command-center/internal/plan/spend"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

const (
	insightsPath   = "/insights"
	insightsChart  = "/insights/chart"
	sparkWidth     = 72.0
	sparkHeight    = 28.0
	sparkMinPoints = 2
)

var insightRanges = []insight.Range{insight.Range7d, insight.Range30d, insight.Range90d}

var insightMetrics = []insight.Metric{insight.MetricTokens, insight.MetricPct, insight.MetricWall, insight.MetricTools}

type InsightsParams struct {
	Params
	Range   insight.Range
	Metric  insight.Metric
	View    insight.View
	Repo    string
	Feature string
}

func ParseInsightsParams(q url.Values) InsightsParams {
	base := ParseParams(q)
	rng, err := insight.ParseRange(q.Get("range"))
	if err != nil {
		rng = insight.Range30d
	}
	metric := insight.Metric(q.Get("metric"))
	if !slices.Contains(insightMetrics, metric) {
		metric = insight.MetricTokens
	}
	view := insight.ViewTicket
	if q.Get("view") == string(insight.ViewWeek) {
		view = insight.ViewWeek
	}
	return InsightsParams{Params: base, Range: rng, Metric: metric, View: view, Repo: base.Repo, Feature: base.Feature}
}

func (p InsightsParams) Days() int {
	days, _ := strconv.Atoi(strings.TrimSuffix(string(p.Range), "d"))
	return days
}

func (p InsightsParams) query() url.Values {
	q := url.Values{}
	set := func(key, value, fallback string) {
		if value != "" && value != fallback {
			q.Set(key, value)
		}
	}
	set("project", p.Project, "")
	set("repo", p.Repo, "")
	set("feature", p.Feature, "")
	set("range", string(p.Range), string(insight.Range30d))
	set("metric", string(p.Metric), string(insight.MetricTokens))
	set("view", string(p.View), string(insight.ViewTicket))
	return q
}

func (p InsightsParams) ViewPath(v insight.View) string {
	return p.path(insightsPath, func(n *InsightsParams) { n.View = v })
}

func (p InsightsParams) path(base string, edit func(*InsightsParams)) string {
	edit(&p)
	return withQuery(base, p.query().Encode())
}

type InsightsPage struct {
	Chrome
	Params   InsightsParams
	Scope    string
	Repos    []ScopeTab
	Features []ScopeTab
	Ranges   []ScopeTab
	Tiles    []TileView
	Cost     insight.Cost
	Limits   []Gauge
	ChartSrc string
	Hotspots []insight.HotspotRow
	Pairs    []insight.StackPair
}

func (p InsightsPage) HotspotShare(tickets int) int {
	top := 0
	for _, h := range p.Hotspots {
		top = max(top, h.Tickets)
	}
	if top == 0 {
		return 0
	}
	return tickets * 100 / top
}

func (InsightsPage) TicketName(ticket string) string {
	name := ticket[strings.LastIndexByte(ticket, '/')+1:]
	if _, err := strconv.Atoi(name); err == nil {
		return "#" + name
	}
	return name
}

type TileView struct {
	insight.Tile
	Label, Figure, Chip string
	Path                string
	Spark               string
}

var tileLabels = map[insight.Metric]string{
	insight.MetricTokens: "Tokens",
	insight.MetricPct:    "% of week",
	insight.MetricWall:   "Wall clock",
	insight.MetricTools:  "Tool calls",
}

func (r *Reader) InsightsPage(ctx context.Context, now time.Time, p InsightsParams) (InsightsPage, error) {
	p.Params.Repo, p.Params.Feature = p.Repo, p.Feature
	f, err := r.loadFrame(ctx, now, p.Params)
	if err != nil {
		return InsightsPage{}, fmt.Errorf("load frame: %w", err)
	}
	p.Params, p.Repo, p.Feature = f.params, f.params.Repo, f.params.Feature

	loc, err := time.LoadLocation(insightsTimezone(now.Location()))
	if err != nil {
		return InsightsPage{}, fmt.Errorf("load timezone: %w", err)
	}
	current, prior := insight.Windows(p.Range, now)
	currentFacts, err := r.store.MergedTicketFacts(ctx, spendQuery(p, loc, current, 0))
	if err != nil {
		return InsightsPage{}, fmt.Errorf("load merged ticket facts: %w", err)
	}
	priorFacts, err := r.store.MergedTicketFacts(ctx, spendQuery(p, loc, prior, -1))
	if err != nil {
		return InsightsPage{}, fmt.Errorf("load prior merged ticket facts: %w", err)
	}
	fit := f.split[agentlog.SevenDay].Fit
	cost := insight.DeriveCost(currentFacts, priorFacts, p.Range, p.Metric, fit)
	conflicts, err := r.store.ConflictFacts(ctx, spendQuery(p, loc, current, 0))
	if err != nil {
		return InsightsPage{}, fmt.Errorf("load conflict facts: %w", err)
	}

	chrome := f.chrome
	chrome.setCurrent(keyInsights)
	chrome.titled("Insights")
	chrome.Crumbs = []Crumb{{Label: "Insights"}}

	repos := make([]ScopeTab, len(f.repos))
	for i, repo := range f.repos {
		repos[i] = ScopeTab{Label: repo.Name, Current: repo.Name == p.Repo}
	}
	var features []ScopeTab
	for _, feature := range distinctFeatures(f.tickets) {
		features = append(features, ScopeTab{Label: feature, Current: feature == p.Feature})
	}
	ranges := make([]ScopeTab, len(insightRanges))
	for i, rng := range insightRanges {
		ranges[i] = ScopeTab{
			Label: string(rng), Current: rng == p.Range,
			Path: p.path(insightsPath, func(n *InsightsParams) { n.Range = rng }),
		}
	}
	return InsightsPage{
		Chrome: chrome, Params: p, Scope: scopeLine(p, cost, len(repos)),
		Repos: repos, Features: features, Ranges: ranges,
		Tiles: buildTiles(p, cost, fit), Cost: cost, Limits: limitsWithReset(chrome.Gauges, now),
		ChartSrc: p.path(insightsChart, func(*InsightsParams) {}),
		Hotspots: insight.Hotspots(conflicts.Conflicts), Pairs: insight.StackPairs(conflicts),
	}, nil
}

func spendQuery(p InsightsParams, loc *time.Location, w insight.Window, endShiftDays int) store.SpendQuery {
	return store.SpendQuery{
		Repo: p.Repo, Feature: p.Feature, Loc: loc,
		Since: civilDate(w.Since.In(loc)), Until: civilDate(w.Until.In(loc)).AddDate(0, 0, endShiftDays),
	}
}

func scopeLine(p InsightsParams, cost insight.Cost, repos int) string {
	n := 0
	if len(cost.Tiles) > 0 {
		n = cost.Tiles[0].N
	}
	noun := "merged tickets"
	if n == 1 {
		noun = "merged ticket"
	}
	line := fmt.Sprintf("%d %s in the last %d days", n, noun, p.Days())
	switch {
	case p.Repo != "":
		return line + " in " + p.Repo
	case repos > 1:
		return fmt.Sprintf("%s across %d repos", line, repos)
	}
	return line
}

func buildTiles(p InsightsParams, cost insight.Cost, fit spend.Result) []TileView {
	populated := 0
	for _, b := range cost.Buckets {
		if b.N > 0 {
			populated++
		}
	}
	tiles := make([]TileView, len(cost.Tiles))
	for i, t := range cost.Tiles {
		tv := TileView{
			Tile: t, Label: tileLabels[t.Metric], Figure: "—", Chip: chipText(t),
			Path: p.path(insightsPath, func(n *InsightsParams) { n.Metric = t.Metric }),
		}
		switch {
		case t.N == 0:
		case t.Metric == insight.MetricPct && cost.Calibrating:
			tv.Figure = fmt.Sprintf("Calibrating (%d intervals)", fit.Samples)
		default:
			tv.Figure = formatMetric(t.Metric, t.Median)
			if populated >= sparkMinPoints {
				tv.Spark = sparkPath(t.Spark)
			}
		}
		tiles[i] = tv
	}
	return tiles
}

func chipText(t insight.Tile) string {
	if t.N == 0 {
		return ""
	}
	change, ok := t.Change.Get()
	if !ok {
		return "n too small"
	}
	arrow := "↗"
	if change.Fraction < 0 {
		arrow = "↘"
	}
	return fmt.Sprintf("%s %+.1f%%", arrow, change.Fraction*100)
}

func formatMetric(m insight.Metric, v float64) string {
	switch m {
	case insight.MetricTokens:
		switch {
		case v >= 1e6:
			return fmt.Sprintf("%.1fM", v/1e6)
		case v >= 1e3:
			return fmt.Sprintf("%.1fk", v/1e3)
		}
		return fmt.Sprintf("%.0f", v)
	case insight.MetricPct:
		return fmt.Sprintf("%.1f%%", v)
	case insight.MetricWall:
		d := time.Duration(v * float64(time.Second)).Round(time.Second)
		switch {
		case d >= time.Hour:
			return fmt.Sprintf("%dh %02dm", int(d.Hours()), int(d.Minutes())%60)
		case d >= time.Minute:
			return fmt.Sprintf("%dm", int(d.Minutes()))
		}
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case insight.MetricTools:
	}
	return fmt.Sprintf("%.0f", v)
}

func sparkPath(xs []float64) string {
	if len(xs) < sparkMinPoints {
		return ""
	}
	const pad = 2.0
	lo, hi := slices.Min(xs), slices.Max(xs)
	span := max(hi-lo, 1e-9)
	return polyline(len(xs),
		func(i int) float64 { return float64(i) * sparkWidth / float64(len(xs)-1) },
		func(i int) float64 { return sparkHeight - pad - (xs[i]-lo)/span*(sparkHeight-2*pad) },
	)
}

func polyline(count int, x, y func(i int) float64) string {
	var b strings.Builder
	for i := range count {
		cmd := "L"
		if i == 0 {
			cmd = "M"
		}
		fmt.Fprintf(&b, "%s %.2f %.2f ", cmd, x(i), y(i))
	}
	return strings.TrimSpace(b.String())
}

func limitsWithReset(gauges []Gauge, now time.Time) []Gauge {
	limits := slices.Clone(gauges)
	for i := range limits {
		if !limits[i].ResetsAt.IsZero() {
			limits[i].ResetsIn = untilReset(now, limits[i].ResetsAt)
		}
	}
	return limits
}

// Postgres dates have no timezone; encode the wall-clock date at UTC midnight.
func civilDate(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// time.Local.String() returns "Local", not an IANA name usable in the query.
func insightsTimezone(loc *time.Location) string {
	if name := loc.String(); name != "Local" {
		return name
	}
	if name := systemTimezoneName(); name != "" {
		return name
	}
	return "UTC"
}

func systemTimezoneName() string {
	target, err := os.Readlink("/etc/localtime")
	if err != nil {
		return ""
	}
	const zoneinfoDir = "zoneinfo/"
	i := strings.Index(target, zoneinfoDir)
	if i < 0 {
		return ""
	}
	return target[i+len(zoneinfoDir):]
}

func untilReset(now, at time.Time) string {
	d := at.Sub(now)
	if d <= 0 {
		return "resetting now"
	}
	d = d.Round(time.Minute)
	if d >= 24*time.Hour {
		return fmt.Sprintf("in %dd %dh", int(d.Hours())/24, int(d.Hours())%24)
	}
	return fmt.Sprintf("in %dh %02dm", int(d.Hours()), int(d.Minutes())%60)
}
