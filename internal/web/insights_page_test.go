package web_test

import (
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cctest"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/htmltest"
)

func TestInsightsPageCarriesRepoAndFeatureScopeThrough(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	insightsTicket(t, store, "sandbox://CC-1", "cc-sandbox", "feat-a")
	repos := named("cc-sandbox")
	server := openServer(track(t, store, repos...), cctest.Clock{At: testNow.Add(31 * 24 * time.Hour)}, "")

	root := htmltest.Parse(t, renderPath(t, server, "/insights?repo=cc-sandbox&feature=feat-a"))
	root.One(t, `a[href="/insights?feature=feat-a"]`)
	crumbs := root.One(t, "ol.crumbs")
	if got := crumbs.Text(); !strings.Contains(got, "feat-a") {
		t.Errorf("breadcrumb = %q, want scoped feature feat-a: %s", got, crumbs.HTML())
	}
}

func TestInsightsPageShowsEmptySpendModules(t *testing.T) {
	t.Parallel()

	main := htmltest.Parse(t, renderPath(t, newServer(openStore(t), testNow), "/insights")).One(t, "main#main")
	main.One(t, `[aria-label="limits"]`)
	main.One(t, `[aria-label="spend split"]`)
	if got := main.Text(); !strings.Contains(got, "no merged tickets in range") {
		t.Errorf("empty insights = %q, want no merged tickets in range", got)
	}
}

func TestInsightsPageRendersMergedTicketsAsAnSVGChart(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	insightsTicket(t, store, "sandbox://CC-1", "cc-sandbox", "feat-a")
	now := testNow.Add(31*24*time.Hour + 3*time.Hour)
	disposeInsightsRun(t, store, "sandbox://CC-1", "agent", now.Add(-time.Hour), 4.20)
	mergeInsightsTicket(t, store, "sandbox://CC-1", now)

	server := newServer(store, now)
	htmltest.Parse(t, renderPath(t, server, "/insights")).One(t, `main#main svg[width]`)
}

func TestInsightsPageShowsEachLimitsResetTime(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	now := testNow.Add(31*24*time.Hour + 3*time.Hour)
	reading := agentlog.Reading{
		Window: agentlog.FiveHour, Utilization: 0.42,
		ResetsAt: now.Add(2*time.Hour + 5*time.Minute), At: now,
	}
	if err := store.RecordReadings(t.Context(), []agentlog.Reading{reading}); err != nil {
		t.Fatalf("RecordReadings: %v", err)
	}

	main := htmltest.Parse(t, renderPath(t, newServer(store, now), "/insights")).One(t, "main#main")
	if got := main.One(t, `time[datetime="2026-09-20T17:05:00Z"]`).Text(); got != "in 2h 05m" {
		t.Errorf("five-hour reset time = %q, want in 2h 05m", got)
	}
	if got := main.Text(); !strings.Contains(got, "no reset time yet") {
		t.Error("weekly reset status missing from insights text: want no reset time yet")
	}
}
