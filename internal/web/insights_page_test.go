package web_test

import (
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/plan/insight"
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
		chart := main.One(t, `section[aria-labelledby="cost-h"] cc-insights-chart`)
		if got := chart.HTML(); !strings.Contains(got, "/insights/chart") {
			t.Errorf("GET /insights chart element = %q, want src to /insights/chart", got)
		}
		main.One(t, `[aria-label="limits"]`)
		if got := main.Text(); !strings.Contains(got, "1 merged ticket in the last 30 days") {
			t.Errorf("GET /insights text = %q, want the scope line", got)
		}
	})

	t.Run("empty conflict ledgers say so", func(t *testing.T) {
		t.Parallel()
		main := htmltest.Parse(t, renderPath(t, newServer(openStore(t), now), "/insights")).One(t, "main#main")
		got := main.Text()
		wants := []string{"Conflict hotspots", "No conflicts recorded in the last 30 days", "No pairs in the last 30 days"}
		for _, want := range wants {
			if !strings.Contains(got, want) {
				t.Errorf("GET /insights text = %q, want %q", got, want)
			}
		}
	})

	t.Run("conflicts fill the hotspot and stacking ledgers", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		wall := time.Now()
		for _, url := range []string{"sandbox://CC-1", "sandbox://CC-2"} {
			storetest.FeatureTicket(t, store, url, "cc-sandbox", "feat-a")
			storetest.Run(t, store, storetest.RunOpts{
				TicketURL: url, Kind: "agent", BaselineSHA: "deadbeef", PromptHash: "h" + url, StartedAt: wall.Add(-time.Hour),
			})
			paths := []insight.ConflictPath{{Path: "internal/a.go"}, {Path: "gen/x.sql", Generated: true}}
			if err := store.RecordRefreshConflict(t.Context(), url, "conflicted", paths); err != nil {
				t.Fatalf("RecordRefreshConflict(%s): %v", url, err)
			}
		}

		main := htmltest.Parse(t, renderPath(t, newServer(store, wall), "/insights")).One(t, "main#main")
		rows := main.One(t, `table[aria-label="conflict hotspots"]`).Text()
		for _, want := range []string{"internal/a.go", "gen/x.sql", "generated"} {
			if !strings.Contains(rows, want) {
				t.Errorf("GET /insights hotspots = %q, want %q", rows, want)
			}
		}
		pairs := main.One(t, "ul.insight-pairs").Text()
		if !strings.Contains(pairs, "CC-1") || !strings.Contains(pairs, "CC-2") || strings.Contains(pairs, "gen/x.sql") {
			t.Errorf("GET /insights pairs = %q, want CC-1 and CC-2 on internal/a.go only", pairs)
		}
	})

	t.Run("empty rework ledgers say so and the rework choice survives in links", func(t *testing.T) {
		t.Parallel()
		root := htmltest.Parse(t, renderPath(t, newServer(openStore(t), now), "/insights?rework=pushes&range=7d"))
		main := root.One(t, "main#main")

		got := main.Text()
		wants := []string{
			"Why runs parked", "No parked runs in the last 7 days",
			"Failing jobs", "No failing jobs in the last 7 days",
		}
		for _, want := range wants {
			if !strings.Contains(got, want) {
				t.Errorf("GET /insights text = %q, want %q", got, want)
			}
		}
		root.One(t, `nav[aria-label="Rework metric"] a[href="/insights?range=7d"]`)
		root.One(t, `nav[aria-label="Rework metric"] a[href="/insights?range=7d&rework=conflicted"]`)
		root.One(t, `nav[aria-label="Range"] a[href="/insights?rework=pushes"]`)
	})

	t.Run("a parked ticket fills the park ledger", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		storetest.FeatureTicket(t, store, "sandbox://CC-1", "cc-sandbox", "feat-a")
		err := store.AppendEvent(t.Context(), storepkg.Event{
			At: now.Add(-time.Hour), TicketURL: "sandbox://CC-1",
			Kind: storepkg.EventParked, Detail: "needs a decision on 3 files",
		})
		if err != nil {
			t.Fatalf("AppendEvent: %v", err)
		}

		main := htmltest.Parse(t, renderPath(t, newServer(store, now), "/insights")).One(t, "main#main")
		if got := main.One(t, `table[aria-label="why runs parked"]`).Text(); !strings.Contains(got, "needs a decision on …") {
			t.Errorf("GET /insights park ledger = %q, want the normalised reason", got)
		}
	})

	t.Run("stage share and the skills ledger follow the repo pick", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		storetest.FeatureTicket(t, store, "sandbox://CC-1", "cc-sandbox", "feat-a")
		id := storetest.Run(t, store, storetest.RunOpts{
			TicketURL: "sandbox://CC-1", Kind: "agent", BaselineSHA: "deadbeef", PromptHash: "h",
			StartedAt: now.Add(-3 * time.Hour),
		})
		metrics := agentlog.RunMetrics{
			Input: 1000, Output: 500,
			Phases: []agentlog.PhaseTokens{{Skill: "tdd", Tokens: 900}, {Skill: "review", Tokens: 300}},
		}
		err := store.RecordDisposition(t.Context(), storepkg.Disposition{
			RunID: plan.RunID(id), Outcome: plan.OutcomePush, Metrics: option.Some(metrics), EndedAt: now.Add(-2 * time.Hour),
		})
		if err != nil {
			t.Fatalf("RecordDisposition: %v", err)
		}
		storetest.Merged(t, store, "sandbox://CC-1", now.Add(-time.Hour))
		server := newServer(track(t, store, named("cc-sandbox")...), now)

		all := htmltest.Parse(t, renderPath(t, server, "/insights")).One(t, "main#main")
		if got := all.Text(); !strings.Contains(got, "Pick a repo above to see its skills.") {
			t.Errorf("GET /insights text = %q, want the pick-a-repo note", got)
		}
		all.One(t, `ul[aria-label="token share by stage"] [data-stage="1"]`)

		one := htmltest.Parse(t, renderPath(t, server, "/insights?repo=cc-sandbox")).One(t, "main#main")
		rows := one.One(t, `table[aria-label="skills"]`).Text()
		for _, want := range []string{"tdd", "review", "100%"} {
			if !strings.Contains(rows, want) {
				t.Errorf("GET /insights?repo skills = %q, want %q", rows, want)
			}
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
