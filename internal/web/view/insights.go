package view

import (
	"cmp"
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/plan/spend"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

const insightsDefaultRangeDays = 30

const insightsDateFormat = "2006-01-02"

type InsightsResponse struct {
	Loc          *time.Location
	Points       []InsightsPoint
	WastePctWeek float64
	Workflows    []WorkflowSeries
}

type InsightsPoint struct {
	Ticket          string
	Title           string
	Feature         string
	MergedAt        time.Time
	PctWeek         float64
	AgentPctWeek    float64
	FollowUpPctWeek float64
}

// Postgres dates have no timezone; encode the wall-clock date at UTC midnight.
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

type InsightsPage struct {
	Chrome
	Chart     InsightsChart
	Limits    []Limit
	Split     SpendSplit
	Merged    []MergedRow
	Workflows []WorkflowSeries
}

type MergedRow struct {
	Name, Feature, Day     string
	Agent, FollowUp, Total float64
}

func buildMergedRows(points []InsightsPoint, loc *time.Location) []MergedRow {
	rows := make([]MergedRow, 0, len(points))
	for i := len(points) - 1; i >= 0; i-- {
		p := points[i]
		rows = append(rows, MergedRow{
			Name: cmp.Or(p.Title, p.Ticket), Feature: p.Feature,
			Day:   p.MergedAt.In(cmp.Or(loc, time.UTC)).Format(insightsDateFormat),
			Agent: p.AgentPctWeek, FollowUp: p.FollowUpPctWeek, Total: p.PctWeek,
		})
	}
	return rows
}

type Limit struct {
	Label     string
	Pct       int
	NoReading bool
	Stale     bool
	ReadAgo   string
	ResetsIn  string
	ResetsAt  string
}

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
		limits[i] = Limit{Label: g.Label, Pct: g.Pct, NoReading: g.NoReading, Stale: g.Stale, ReadAgo: g.ReadAgo}
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

func buildSpendSplit(points []InsightsPoint) SpendSplit {
	var agent, followUp, total float64
	for _, p := range points {
		agent += p.AgentPctWeek
		followUp += p.FollowUpPctWeek
		total += p.PctWeek
	}
	return SpendSplit{
		Rows: []SplitRow{
			{Kind: "agent", Pct: agent},
			{Kind: "follow-up", Pct: followUp},
		},
		Total: agent + followUp,
	}
}

func (r *Reader) InsightsPage(ctx context.Context, now time.Time, q url.Values) (InsightsPage, error) {
	chrome, err := r.Chrome(ctx, now, ParseParams(q))
	if err != nil {
		return InsightsPage{}, fmt.Errorf("load chrome: %w", err)
	}
	chrome.setCurrent(keyInsights)
	chrome.titled("Insights")
	chrome.Crumbs = []Crumb{{Label: "Insights"}}
	resp, err := r.Insights(ctx, now, q)
	if err != nil {
		return InsightsPage{}, fmt.Errorf("load insights: %w", err)
	}
	return NewInsightsPage(chrome, resp, now), nil
}

func NewInsightsPage(chrome Chrome, resp InsightsResponse, now time.Time) InsightsPage {
	return InsightsPage{
		Chrome: chrome, Chart: buildInsightsChart(resp),
		Limits: buildLimits(chrome.Gauges, now), Split: buildSpendSplit(resp.Points),
		Merged: buildMergedRows(resp.Points, resp.Loc), Workflows: resp.Workflows,
	}
}

func InsightsRange(now time.Time, rawSince string) (since, until time.Time, timezone string) {
	until = civilDate(now)
	return parseSinceOrDefault(rawSince, until), until, insightsTimezone(now.Location())
}

func (r *Reader) Insights(ctx context.Context, now time.Time, q url.Values) (InsightsResponse, error) {
	since, until, tz := InsightsRange(now, q.Get("since"))
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return InsightsResponse{}, fmt.Errorf("load timezone %s: %w", tz, err)
	}
	spendQuery := store.SpendQuery{
		Repo: q.Get("repo"), Feature: q.Get("feature"), Loc: loc, Since: since, Until: until,
	}

	merged, err := r.store.MergedTicketSpend(ctx, spendQuery)
	if err != nil {
		return InsightsResponse{}, fmt.Errorf("load merged ticket spend: %w", err)
	}
	wasteUSD, err := r.store.WithdrawnTicketWaste(ctx, spendQuery)
	if err != nil {
		return InsightsResponse{}, fmt.Errorf("load withdrawn ticket waste: %w", err)
	}
	workflowSpend, err := r.store.MergedTicketWorkflowSpend(ctx, spendQuery)
	if err != nil {
		return InsightsResponse{}, fmt.Errorf("load workflow spend: %w", err)
	}
	withdrawn, err := r.store.WithdrawnWorkflowTickets(ctx, spendQuery)
	if err != nil {
		return InsightsResponse{}, fmt.Errorf("load withdrawn workflow tickets: %w", err)
	}
	fits, err := r.store.FitFactors(ctx, now)
	if err != nil {
		return InsightsResponse{}, fmt.Errorf("load fit factors: %w", err)
	}
	factor := fits[agentlog.SevenDay].Factor

	resp := InsightsResponse{
		Loc:    loc,
		Points: make([]InsightsPoint, len(merged)), WastePctWeek: spend.PctWeek(wasteUSD, factor),
		Workflows: buildWorkflowSeries(workflowSpend, withdrawn, factor),
	}
	for i, p := range merged {
		agentPct, followUpPct, totalPct := spend.KindPctWeek(p.AgentUSD, p.FollowUpUSD, factor)
		resp.Points[i] = InsightsPoint{
			Ticket: p.Ticket, Title: p.Title, Feature: p.Feature, MergedAt: p.MergedAt,
			PctWeek:         totalPct,
			AgentPctWeek:    agentPct,
			FollowUpPctWeek: followUpPct,
		}
	}
	return resp, nil
}
