package web_test

import (
	"slices"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/htmltest"
	"github.com/O-Marsters-1997/command-center/internal/web"
)

func TestRailRendersTheLatestStoredReading(t *testing.T) {
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
		t.Fatal(err)
	}

	server := newServer(store, observedAt.Add(45*time.Second))
	footer := htmltest.Parse(t, renderPath(t, server, "/rail")).One(t, "footer.rail-footer")
	gauges := footer.Find("span.rail-gauge")
	if len(gauges) != 2 {
		t.Fatalf("rail gauges = %d, want 2", len(gauges))
	}
	for i, want := range []struct{ label, pct, width string }{
		{label: "five-hour", pct: "42%", width: "width: 42%"},
		{label: "weekly", pct: "0%", width: "width: 0%"},
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

func TestRailGaugesSurviveARepeatedPollWithoutFlicker(t *testing.T) {
	t.Parallel()

	observedAt := testNow
	store := seededStore(t, observedAt)
	reading := agentlog.Reading{
		Window: agentlog.SevenDay, Utilization: 0.19,
		ResetsAt: observedAt.Add(24 * time.Hour), At: observedAt,
	}
	if err := store.RecordReadings(t.Context(), []agentlog.Reading{reading}); err != nil {
		t.Fatal(err)
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
	if !slices.Equal(first, second) {
		t.Errorf("gauge markup changed between polls: first = %q, second = %q", first, second)
	}
}

func TestSpendLimitNotice(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name        string
		utilization float64
		want        string
	}{
		{
			name: "above limit", utilization: 0.82,
			want: "spawning paused: five-hour usage is 82%, at or above the spend_limit_5h of 80%.",
		},
		{name: "below limit", utilization: 0.50, want: ""},
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
				t.Fatal(err)
			}

			server := newServer(store, observedAt.Add(45*time.Second), web.WithSpendLimit5h(80))
			notices := htmltest.Parse(t, renderPath(t, server, "/tickets")).One(t, "#notices")
			if got := notices.Text(); got != tt.want {
				t.Errorf("notices.Text() = %q, want %q in %s", got, tt.want, notices.HTML())
			}
		})
	}
}
