package web_test

import (
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/web/htmltest"
)

func TestInsightsPage(t *testing.T) {
	t.Parallel()

	t.Run("an empty range renders the limits and spend split modules", func(t *testing.T) {
		t.Parallel()
		main := htmltest.Parse(t, renderPath(t, newServer(openStore(t), testNow), "/insights")).One(t, "main#main")

		main.One(t, `[aria-label="limits"]`)
		main.One(t, `[aria-label="spend split"]`)
		if got := main.Text(); !strings.Contains(got, "no merged tickets in range") {
			t.Errorf("GET /insights text = %q, want it to say no merged tickets in range", got)
		}
	})

	t.Run("carries the repo and feature scope through", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		insightsTicket(t, store, "sandbox://CC-1", "cc-sandbox", "feat-a")
		server := openServer(track(t, store, named("cc-sandbox")...), fixedClock(testNow.Add(31*24*time.Hour)), "")

		root := htmltest.Parse(t, renderPath(t, server, "/insights?repo=cc-sandbox&feature=feat-a"))
		root.One(t, `a[href="/insights?feature=feat-a"]`)
		if got := root.One(t, "ol.crumbs").Text(); strings.TrimSpace(got) != "Insights" {
			t.Errorf("GET /insights?feature=feat-a breadcrumb = %q, want a single Insights crumb", got)
		}
	})

	t.Run("renders merged tickets as an SVG chart", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		insightsTicket(t, store, "sandbox://CC-1", "cc-sandbox", "feat-a")
		now := testNow.Add(31*24*time.Hour + 3*time.Hour)
		disposeInsightsRun(t, store, "sandbox://CC-1", "agent", now.Add(-time.Hour), 4.20)
		mergeInsightsTicket(t, store, "sandbox://CC-1", now)

		htmltest.Parse(t, renderPath(t, newServer(store, now), "/insights")).One(t, `main#main svg[width]`)
	})

	t.Run("shows each limit's reset time", func(t *testing.T) {
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
			t.Errorf("GET /insights five-hour reset = %q, want %q", got, "in 2h 05m")
		}
		if got := main.Text(); !strings.Contains(got, "no reset time yet") {
			t.Errorf("GET /insights text = %q, want the weekly limit to say no reset time yet", got)
		}
	})
}
