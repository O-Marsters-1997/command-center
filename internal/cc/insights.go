package cc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc/ccdb"
)

const insightsDefaultRangeDays = 30

const insightsDateFormat = "2006-01-02"

// InsightsBucket is one local calendar day's aggregated run spend.
type InsightsBucket struct {
	Day       time.Time
	TokensIn  int64
	TokensOut int64
	Runs      int64
	Unsettled int64
}

// RunInsights buckets every disposed run in [since, until] by local day. A run's feature comes
// from its own ticket alone, never ADR 13's blocker closure, and a withdrawn ticket's runs still
// count (docs/adr/0015-run-metrics-are-captured-at-disposition-from-stdout.md).
func (s *Store) RunInsights(
	ctx context.Context, repo, feature, tz string, since, until time.Time,
) ([]InsightsBucket, error) {
	rows, err := s.q.RunInsights(ctx, ccdb.RunInsightsParams{
		Since: since, Until: until, Timezone: tz, Repo: repo, Feature: feature,
	})
	if err != nil {
		return nil, fmt.Errorf("select run insights: %w", err)
	}

	buckets := make([]InsightsBucket, len(rows))
	for i, row := range rows {
		buckets[i] = InsightsBucket{
			Day: row.Day, TokensIn: row.TokensIn, TokensOut: row.TokensOut,
			Runs: row.Runs, Unsettled: row.Unsettled,
		}
	}
	return buckets, nil
}

type insightsResponse struct {
	Since    string               `json:"since"`
	Until    string               `json:"until"`
	Timezone string               `json:"timezone"`
	Buckets  []insightsBucketJSON `json:"buckets"`
}

type insightsBucketJSON struct {
	Day       string `json:"day"`
	TokensIn  int64  `json:"tokens_in"`
	TokensOut int64  `json:"tokens_out"`
	Runs      int64  `json:"runs"`
	Unsettled int64  `json:"unsettled"`
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

// handleInsights serves GET /insights.json?repo=&feature=&since=, the daily spend series.
func (s *Server) handleInsights(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	now := s.now()
	tz := insightsTimezone(now.Location())
	until := civilDate(now)
	since := parseSinceOrDefault(q.Get("since"), until)

	buckets, err := s.store.RunInsights(r.Context(), q.Get("repo"), q.Get("feature"), tz, since, until)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := insightsResponse{
		Since: since.Format(insightsDateFormat), Until: until.Format(insightsDateFormat), Timezone: tz,
		Buckets: make([]insightsBucketJSON, len(buckets)),
	}
	for i, b := range buckets {
		resp.Buckets[i] = insightsBucketJSON{
			Day: b.Day.Format(insightsDateFormat), TokensIn: b.TokensIn, TokensOut: b.TokensOut,
			Runs: b.Runs, Unsettled: b.Unsettled,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
