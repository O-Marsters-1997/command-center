package view

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/spend"
)

const insightsDefaultRangeDays = 30

const insightsDateFormat = "2006-01-02"

// InsightsResponse is GET /insights.json: each merged ticket in the window as a share of the
// weekly limit.
type InsightsResponse struct {
	Since        string              `json:"since"`
	Until        string              `json:"until"`
	Timezone     string              `json:"timezone"`
	Points       []insightsPointJSON `json:"points"`
	WastePctWeek float64             `json:"waste_pct_week"`
}

type insightsPointJSON struct {
	Ticket          string  `json:"ticket"`
	Title           string  `json:"title"`
	MergedAt        string  `json:"merged_at"`
	PctWeek         float64 `json:"pct_week"`
	AgentPctWeek    float64 `json:"agent_pct_week"`
	ResolvePctWeek  float64 `json:"resolve_pct_week"`
	FollowUpPctWeek float64 `json:"follow_up_pct_week"`
}

// civilDate strips t to its own wall-clock year, month and day, encoded at UTC midnight since
// Postgres's date type carries no zone of its own.
func civilDate(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func parseSinceOrDefault(raw string, until time.Time) time.Time {
	since, err := time.Parse(insightsDateFormat, raw)
	if err != nil {
		return until.AddDate(0, 0, -insightsDefaultRangeDays)
	}
	return since
}

// insightsTimezone names loc for the query and the response. time.Local's String is always the
// literal "Local", never an IANA name, so that case alone falls through to systemTimezoneName.
func insightsTimezone(loc *time.Location) string {
	if name := loc.String(); name != "Local" {
		return name
	}
	if name := systemTimezoneName(); name != "" {
		return name
	}
	return "UTC"
}

// systemTimezoneName resolves the OS's configured zone to an IANA name: TZ overrides time.Local,
// else /etc/localtime is a symlink into the zoneinfo tree.
func systemTimezoneName() string {
	if tz := os.Getenv("TZ"); tz != "" {
		return tz
	}
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

type InsightsPage struct {
	Chrome
	Chart  InsightsChart
	Limits []Limit
	Split  SpendSplit
}

// Limit is one rate-limit window on the insights page. ResetsIn is empty when no reading has
// reported a reset time.
type Limit struct {
	Label    string
	Pct      int
	ResetsIn string
	ResetsAt string
}

// SpendSplit is the chart's spend by kind, as a share of the weekly limit. Its rows sum to Total,
// the sum of the chart's dots.
type SpendSplit struct {
	Rows  []SplitRow
	Total float64
}

type SplitRow struct {
	Kind string
	Pct  float64
}

func buildLimits(gauges []Gauge, now time.Time) []Limit {
	limits := make([]Limit, len(gauges))
	for i, g := range gauges {
		limits[i] = Limit{Label: g.Label, Pct: g.Pct}
		if g.ResetsAt.IsZero() {
			continue
		}
		limits[i].ResetsAt = g.ResetsAt.UTC().Format(time.RFC3339)
		limits[i].ResetsIn = untilReset(now, g.ResetsAt)
	}
	return limits
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

func buildSpendSplit(points []insightsPointJSON) SpendSplit {
	var agent, resolve, followUp, total float64
	for _, p := range points {
		agent += p.AgentPctWeek
		resolve += p.ResolvePctWeek
		followUp += p.FollowUpPctWeek
		total += p.PctWeek
	}
	explore := max(0, total-agent-resolve-followUp)
	return SpendSplit{
		Rows: []SplitRow{
			{Kind: "agent", Pct: agent},
			{Kind: "resolve", Pct: resolve},
			{Kind: "follow-up", Pct: followUp},
			{Kind: "explore", Pct: explore},
		},
		Total: agent + resolve + followUp + explore,
	}
}

func (r *Reader) InsightsPage(ctx context.Context, now time.Time, q url.Values) (InsightsPage, error) {
	chrome, err := r.Chrome(ctx, now, ParseParams(q))
	if err != nil {
		return InsightsPage{}, err
	}
	chrome.Section = "insights"
	resp, err := r.Insights(ctx, now, q)
	if err != nil {
		return InsightsPage{}, err
	}
	return InsightsPage{
		Chrome: chrome, Chart: buildInsightsChart(resp),
		Limits: buildLimits(chrome.Gauges, now), Split: buildSpendSplit(resp.Points),
	}, nil
}

// Insights reads the merged-ticket spend between ?since= (default thirty days back) and today in
// now's own zone, narrowed by ?repo= and ?feature=.
func (r *Reader) Insights(ctx context.Context, now time.Time, q url.Values) (InsightsResponse, error) {
	tz := insightsTimezone(now.Location())
	until := civilDate(now)
	since := parseSinceOrDefault(q.Get("since"), until)
	repo, feature := q.Get("repo"), q.Get("feature")

	merged, err := r.store.MergedTicketSpend(ctx, repo, feature, tz, since, until)
	if err != nil {
		return InsightsResponse{}, err
	}
	wasteUSD, err := r.store.WithdrawnTicketWaste(ctx, repo, feature, tz, since, until)
	if err != nil {
		return InsightsResponse{}, err
	}
	fits, err := r.store.FitFactors(ctx, now)
	if err != nil {
		return InsightsResponse{}, err
	}
	factor := fits[agentlog.SevenDay].Factor

	resp := InsightsResponse{
		Since: since.Format(insightsDateFormat), Until: until.Format(insightsDateFormat), Timezone: tz,
		Points: make([]insightsPointJSON, len(merged)), WastePctWeek: spend.PctWeek(wasteUSD, factor),
	}
	for i, p := range merged {
		agentPct, resolvePct, followUpPct, totalPct := spend.KindPctWeek(p.AgentUSD, p.ResolveUSD, p.FollowUpUSD, factor)
		totalPct += spend.PctWeek(p.ExploreUSD, factor)
		resp.Points[i] = insightsPointJSON{
			Ticket: p.Ticket, Title: p.Title, MergedAt: p.MergedAt.UTC().Format(time.RFC3339),
			PctWeek:         totalPct,
			AgentPctWeek:    agentPct,
			ResolvePctWeek:  resolvePct,
			FollowUpPctWeek: followUpPct,
		}
	}
	return resp, nil
}
