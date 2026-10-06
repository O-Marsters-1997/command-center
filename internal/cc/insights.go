package cc

import (
	_ "embed"
	"encoding/json"
	"html/template"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/spend"
)

const insightsDefaultRangeDays = 30

const insightsDateFormat = "2006-01-02"

type insightsResponse struct {
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

//go:embed insights.tmpl
var insightsPageSource string

var insightsPage = template.Must(page.New("insights").Parse(insightsPageSource))

type insightsPageView struct {
	chrome
}

func (s *Server) handleInsightsPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	chr, err := s.chromeFor(ctx, parseViewParams(r.URL.Query()))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	chr.Section = "insights"

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := insightsPage.Execute(w, insightsPageView{chrome: chr}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleInsights(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	now := s.clock.Now()
	tz := insightsTimezone(now.Location())
	until := civilDate(now)
	since := parseSinceOrDefault(q.Get("since"), until)
	repo, feature := q.Get("repo"), q.Get("feature")

	merged, err := s.store.MergedTicketSpend(ctx, repo, feature, tz, since, until)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	wasteUSD, err := s.store.WithdrawnTicketWaste(ctx, repo, feature, tz, since, until)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	fits, err := s.store.FitFactors(ctx, now)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	factor := fits[agentlog.SevenDay].Factor

	resp := insightsResponse{
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

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
