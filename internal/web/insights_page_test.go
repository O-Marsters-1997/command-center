package web_test

import (
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
)

func TestInsightsPageCarriesRepoAndFeatureScopeThrough(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	insightsTicket(t, store, "sandbox://CC-1", "cc-sandbox", "feat-a")
	repos := named("cc-sandbox")
	server := openServer(track(t, store, repos...), fixedClock(time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)), "")

	got := renderPath(t, server, "/insights?repo=cc-sandbox&feature=feat-a")
	if !strings.Contains(got, `href="/insights?feature=feat-a"`) {
		t.Errorf("sidebar insights link does not carry the feature scope through:\n%s", got)
	}
	if !strings.Contains(got, ">feat-a<") {
		t.Errorf("breadcrumb does not show the scoped feature:\n%s", got)
	}
}

func TestInsightsPageRendersMergedTicketsAsAnSVGChart(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	insightsTicket(t, store, "sandbox://CC-1", "cc-sandbox", "feat-a")
	now := time.Date(2026, 9, 20, 15, 0, 0, 0, time.UTC)
	disposeInsightsRun(t, store, "sandbox://CC-1", "agent", now.Add(-time.Hour), 4.20)
	mergeInsightsTicket(t, store, "sandbox://CC-1", now)

	server := newServer(store, now)
	got := renderPath(t, server, "/insights")
	if want := `<svg width=`; !strings.Contains(got, want) {
		t.Errorf("insights page with a merged ticket lacks the chart %s:\n%s", want, got)
	}
}

func TestInsightsPageShowsEachLimitsResetTime(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	now := time.Date(2026, 9, 20, 15, 0, 0, 0, time.UTC)
	reading := agentlog.Reading{
		Window: agentlog.FiveHour, Utilization: 0.42,
		ResetsAt: now.Add(2*time.Hour + 5*time.Minute), At: now,
	}
	if err := store.RecordReadings(t.Context(), []agentlog.Reading{reading}); err != nil {
		t.Fatal(err)
	}

	got := renderPath(t, newServer(store, now), "/insights")
	if !strings.Contains(got, `datetime="2026-09-20T17:05:00Z">in 2h 05m</time>`) {
		t.Errorf("five-hour limit does not show its reset time:\n%s", got)
	}
	if !strings.Contains(got, "no reset time yet") {
		t.Errorf("weekly limit with no reading does not say so:\n%s", got)
	}
}
