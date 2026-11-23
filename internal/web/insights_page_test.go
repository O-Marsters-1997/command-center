package web_test

import (
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
	"github.com/O-Marsters-1997/command-center/internal/web/htmltest"
)

func TestInsightsPage(t *testing.T) {
	t.Parallel()
	now := testNow.Add(31*24*time.Hour + 3*time.Hour)

	t.Run("an empty range shows dashes, says so and keeps the limits", func(t *testing.T) {
		t.Parallel()
		main := htmltest.Parse(t, renderPath(t, newServer(openStore(t), testNow), "/insights")).One(t, "main#main")

		main.One(t, `[aria-label="limits"]`)
		got := main.Text()
		if !strings.Contains(got, "No merged tickets in the last 30 days") {
			t.Errorf("GET /insights text = %q, want it to say no merged tickets in the last 30 days", got)
		}
		for _, tile := range main.Find(`nav[aria-label="Metric"] a`) {
			if !strings.Contains(tile.Text(), "—") {
				t.Errorf("GET /insights tile = %q, want a dash figure", tile.Text())
			}
		}
	})

	t.Run("carries the repo and feature scope through", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		storetest.FeatureTicket(t, store, "sandbox://CC-1", "cc-sandbox", "feat-a")
		server := newServer(track(t, store, named("cc-sandbox")...), now)

		root := htmltest.Parse(t, renderPath(t, server, "/insights?repo=cc-sandbox&feature=feat-a"))
		root.One(t, `a[href="/insights?feature=feat-a"]`)
		if got := root.One(t, "ol.crumbs").Text(); strings.TrimSpace(got) != "Insights" {
			t.Errorf("GET /insights?feature=feat-a breadcrumb = %q, want a single Insights crumb", got)
		}
		root.One(t, `a[href="/insights?feature=feat-a&metric=pct&repo=cc-sandbox"]`)
	})

	t.Run("range, metric and view survive in every link and swap main", func(t *testing.T) {
		t.Parallel()
		root := htmltest.Parse(t, renderPath(t, newServer(openStore(t), now), "/insights?range=7d&metric=wall&view=week"))

		for _, sel := range []string{
			`a[href="/insights?range=7d&view=week"]`,
			`a[href="/insights?metric=wall&range=90d&view=week"]`,
			`a[href="/insights?metric=wall&range=7d"]`,
		} {
			link := root.One(t, sel)
			if target, _ := link.Attr("hx-target"); target != "#main" {
				t.Errorf("GET /insights link %s hx-target = %q, want #main", sel, target)
			}
		}
		if _, ok := root.One(t, `nav[aria-label="Metric"] a[aria-current="page"]`).Attr("href"); !ok {
			t.Errorf("GET /insights metric tab has no href")
		}
	})

	t.Run("a populated page shows figures, a chip note, the chart island and the limits", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		storetest.FeatureTicket(t, store, "sandbox://CC-1", "cc-sandbox", "feat-a")
		id := storetest.Run(t, store, storetest.RunOpts{
			TicketURL: "sandbox://CC-1", Kind: "agent", BaselineSHA: "deadbeef", PromptHash: "h",
			StartedAt: now.Add(-3 * time.Hour),
		})
		err := store.RecordDisposition(t.Context(), storepkg.Disposition{
			RunID: plan.RunID(id), Outcome: plan.OutcomePush, Cost: option.Some(4.20),
			Metrics: option.Some(agentlog.RunMetrics{Input: 1000, Output: 500, Tools: map[string]int{"Bash": 7}}),
			EndedAt: now.Add(-2 * time.Hour),
		})
		if err != nil {
			t.Fatalf("RecordDisposition: %v", err)
		}
		storetest.Merged(t, store, "sandbox://CC-1", now.Add(-time.Hour))

		main := htmltest.Parse(t, renderPath(t, newServer(store, now), "/insights")).One(t, "main#main")

		tiles := main.One(t, `nav[aria-label="Metric"]`).Text()
		for _, want := range []string{"1.5k", "Calibrating (0 intervals)", "2h 00m", "n too small"} {
			if !strings.Contains(tiles, want) {
				t.Errorf("GET /insights tiles = %q, want %q", tiles, want)
			}
		}
		if got := main.One(t, `cc-insights-chart`).HTML(); !strings.Contains(got, "/insights/chart") {
			t.Errorf("GET /insights chart element = %q, want src to /insights/chart", got)
		}
		main.One(t, `[aria-label="limits"]`)
		if got := main.Text(); !strings.Contains(got, "1 merged ticket in the last 30 days") {
			t.Errorf("GET /insights text = %q, want the scope line", got)
		}
	})

	t.Run("shows each limit's reset time", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
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
		if got := main.Text(); !strings.Contains(got, "reset unknown") {
			t.Errorf("GET /insights text = %q, want the weekly limit to say reset unknown", got)
		}
	})
}
