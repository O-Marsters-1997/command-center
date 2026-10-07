package view

import (
	"context"
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

// insightsTimezone names loc for both the query's AT TIME ZONE argument and the response's own
// field. time.Local's own String is always the literal "Local" -- by the time package's own
// design, never the system's real IANA name -- so that one case alone falls through to
// systemTimezoneName; any other Location (a test's explicit LoadLocation, for one) already
// carries its own real name.
func insightsTimezone(loc *time.Location) string {
	if name := loc.String(); name != "Local" {
		return name
	}
	if name := systemTimezoneName(); name != "" {
		return name
	}
	return "UTC"
}

// systemTimezoneName resolves the OS's own configured zone to an IANA name. TZ, checked first,
// is what actually overrides time.Local's zone data; /etc/localtime, the fallback, is a symlink
// into the zoneinfo tree on every platform this daemon targets.
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

// InsightsPage is the insights page's view model: the chrome, the chart loads its own data.
type InsightsPage struct {
	Chrome
}

// InsightsPage builds the insights shell.
func (r *Reader) InsightsPage(ctx context.Context, now time.Time, params Params) (InsightsPage, error) {
	chrome, err := r.Chrome(ctx, now, params)
	if err != nil {
		return InsightsPage{}, err
	}
	chrome.Section = "insights"
	return InsightsPage{Chrome: chrome}, nil
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
