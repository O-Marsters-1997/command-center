package web_test

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/web"
	"github.com/O-Marsters-1997/command-center/internal/web/htmltest"
)

func testRailLatestReading(t *testing.T) {
	t.Parallel()

	observedAt := testNow
	store := seededStore(t, observedAt)

	older := agentlog.Reading{
		Window: agentlog.FiveHour, Utilization: 0.10,
		ResetsAt: observedAt.Add(time.Hour), At: observedAt.Add(-time.Hour),
	}
	newer := agentlog.Reading{
		Window: agentlog.FiveHour, Utilization: 0.42,
		ResetsAt: observedAt.Add(2 * time.Hour), At: observedAt,
	}
	if err := store.RecordReadings(t.Context(), []agentlog.Reading{older, newer}); err != nil {
		t.Fatalf("RecordReadings: %v", err)
	}

	server := newServer(store, observedAt.Add(45*time.Second))
	footer := htmltest.Parse(t, renderPath(t, server, "/rail")).One(t, "footer.rail-footer")
	gauges := footer.Find("span.rail-gauge")
	if len(gauges) != 2 {
		t.Fatalf("rail gauges = %d, want 2", len(gauges))
	}
	for i, want := range []struct{ label, pct, width string }{
		{label: "five-hour", pct: "42%", width: "width: 42%"},
		{label: "weekly", pct: "no reading", width: "width: 0%"},
	} {
		gauge := gauges[i]
		if got := gauge.Find("span")[0].Text(); got != want.label {
			t.Errorf("gauge label = %q, want %q in %s", got, want.label, gauge.HTML())
		}
		if got := gauge.One(t, "span.rail-pct").Text(); got != want.pct {
			t.Errorf("%s gauge percentage = %q, want %q in %s", want.label, got, want.pct, gauge.HTML())
		}
		fill := gauge.One(t, "span.meter span.meter-fill")
		if got, _ := fill.Attr("style"); got != want.width {
			t.Errorf("%s gauge fill width = %q, want %q in %s", want.label, got, want.width, gauge.HTML())
		}
	}
}

func testRailStaleReading(t *testing.T) {
	t.Parallel()

	store := seededStore(t, testNow)
	reading := agentlog.Reading{
		Window: agentlog.FiveHour, Utilization: 0.42,
		ResetsAt: testNow.Add(time.Hour), At: testNow.Add(-3 * time.Hour),
	}
	if err := store.RecordReadings(t.Context(), []agentlog.Reading{reading}); err != nil {
		t.Fatalf("RecordReadings: %v", err)
	}

	server := newServer(store, testNow)
	gauge := htmltest.Parse(t, renderPath(t, server, "/rail")).One(t, "footer.rail-footer").Find("span.rail-gauge")[0]
	if got := gauge.One(t, "span.rail-pct").Text(); got != "42% · read 3h ago" {
		t.Errorf("stale gauge text = %q, want %q in %s", got, "42% · read 3h ago", gauge.HTML())
	}
	if _, stale := gauge.One(t, "span.meter").Attr("data-stale"); !stale {
		t.Errorf("stale gauge meter lacks data-stale in %s", gauge.HTML())
	}
}

func testInsightsNoReading(t *testing.T) {
	t.Parallel()

	server := newServer(seededStore(t, testNow), testNow)
	limits := htmltest.Parse(t, renderPath(t, server, "/insights")).One(t, "ul.limits").Text()
	for _, want := range []string{"No reading", "reset unknown"} {
		if !strings.Contains(limits, want) {
			t.Errorf("GET /insights limits = %q, want it to contain %q", limits, want)
		}
	}
	if strings.Contains(limits, "0%") {
		t.Errorf("GET /insights limits = %q, want no 0%% without a reading", limits)
	}
}

func testRailGaugesStable(t *testing.T) {
	t.Parallel()

	observedAt := testNow
	store := seededStore(t, observedAt)
	reading := agentlog.Reading{
		Window: agentlog.SevenDay, Utilization: 0.19,
		ResetsAt: observedAt.Add(24 * time.Hour), At: observedAt,
	}
	if err := store.RecordReadings(t.Context(), []agentlog.Reading{reading}); err != nil {
		t.Fatalf("RecordReadings: %v", err)
	}

	server := newServer(store, observedAt.Add(45*time.Second))
	markup := func() []string {
		footer := htmltest.Parse(t, renderPath(t, server, "/rail")).One(t, "footer.rail-footer")
		gauges := footer.Find("span.rail-gauge")
		if len(gauges) != 2 {
			t.Fatalf("rail gauges = %d, want 2", len(gauges))
		}
		return []string{gauges[0].HTML(), gauges[1].HTML()}
	}
	first, second := markup(), markup()
	if diff := cmp.Diff(first, second); diff != "" {
		t.Errorf("GET /rail gauges changed between polls (-first +second):\n%s", diff)
	}
}

func testLayoutSpendLimitNotice(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name        string
		utilization float64
		wantPaused  bool
	}{
		{name: "above limit", utilization: 0.82, wantPaused: true},
		{name: "below limit", utilization: 0.50},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			observedAt := testNow
			store := seededStore(t, observedAt)
			reading := agentlog.Reading{
				Window: agentlog.FiveHour, Utilization: tt.utilization,
				ResetsAt: observedAt.Add(time.Hour), At: observedAt,
			}
			if err := store.RecordReadings(t.Context(), []agentlog.Reading{reading}); err != nil {
				t.Fatalf("RecordReadings: %v", err)
			}

			server := web.NewServer(web.Deps{
				Logger: slog.New(slog.DiscardHandler), Store: store,
				Clock: fixedClock(observedAt.Add(45 * time.Second)), SpendLimit5h: 80,
			})
			notices := htmltest.Parse(t, renderPath(t, server, "/tickets")).One(t, "#notices")
			if paused := strings.Contains(notices.Text(), "spawning paused"); paused != tt.wantPaused {
				t.Errorf("GET /tickets notices = %q, paused %v, want %v", notices.Text(), paused, tt.wantPaused)
			}
		})
	}
}
