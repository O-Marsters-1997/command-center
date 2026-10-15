package view_test

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

func TestNewInsightsPage(t *testing.T) {
	t.Parallel()

	t.Run("no points draws nothing", func(t *testing.T) {
		t.Parallel()
		page := view.NewInsightsPage(view.Chrome{}, view.InsightsResponse{WastePctWeek: 3}, testNow)
		if diff := cmp.Diff(view.InsightsChart{}, page.Chart); diff != "" {
			t.Errorf("NewInsightsPage(no points).Chart mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("chart ticks, dots and waste share one scale", func(t *testing.T) {
		t.Parallel()
		resp := view.InsightsResponse{
			Points:       []view.InsightsPoint{{Ticket: "A", PctWeek: 9}, {Ticket: "B", PctWeek: 2}},
			WastePctWeek: 4,
		}
		chart := view.NewInsightsPage(view.Chrome{}, resp, testNow).Chart

		var labels []string
		for _, tick := range chart.Ticks {
			labels = append(labels, tick.Label)
		}
		if diff := cmp.Diff([]string{"0%", "5%", "10%"}, labels); diff != "" {
			t.Errorf("NewInsightsPage() tick labels mismatch (-want +got):\n%s", diff)
		}
		if got, want := chart.WasteY, 123.2; !cmp.Equal(got, want, cmpopts.EquateApprox(0, 1e-9)) || !chart.HasWaste {
			t.Errorf("NewInsightsPage() waste = {HasWaste: %v, WasteY: %v}, want {true %v}", chart.HasWaste, got, want)
		}
		if chart.Dots[0].Y >= chart.Dots[1].Y {
			t.Errorf("NewInsightsPage() dot Y = %v, %v, want the 9%% point above the 2%% point", chart.Dots[0].Y, chart.Dots[1].Y)
		}
	})

	t.Run("all-zero points still draw a zero tick", func(t *testing.T) {
		t.Parallel()
		resp := view.InsightsResponse{Points: []view.InsightsPoint{{Ticket: "A"}}}
		chart := view.NewInsightsPage(view.Chrome{}, resp, testNow).Chart
		if len(chart.Ticks) != 1 || chart.Ticks[0].Label != "0%" {
			t.Errorf("NewInsightsPage(zero) ticks = %+v, want one 0%% tick", chart.Ticks)
		}
	})

	t.Run("the median trails the points", func(t *testing.T) {
		t.Parallel()
		resp := view.InsightsResponse{Points: []view.InsightsPoint{{PctWeek: 1}, {PctWeek: 5}, {PctWeek: 2}, {PctWeek: 8}}}
		chart := view.NewInsightsPage(view.Chrome{}, resp, testNow).Chart
		want := "M 135.50 176.00 L 302.50 128.00 L 469.50 152.00 L 636.50 116.00"
		if chart.MedianPath != want {
			t.Errorf("NewInsightsPage(1,5,2,8).Chart.MedianPath = %q, want %q (medians 1, 3, 2, 3.5)", chart.MedianPath, want)
		}
	})

	t.Run("limits", func(t *testing.T) {
		t.Parallel()
		chrome := view.Chrome{Gauges: []view.Gauge{
			{Label: "five-hour", Pct: 40, ResetsAt: testNow.Add(26*time.Hour + 10*time.Minute)},
			{Label: "session", ResetsAt: testNow.Add(3*time.Hour + 5*time.Minute)},
			{Label: "past", ResetsAt: testNow.Add(-time.Minute)},
			{Label: "weekly"},
		}}
		got := view.NewInsightsPage(chrome, view.InsightsResponse{}, testNow).Limits
		want := []view.Limit{
			{Label: "five-hour", Pct: 40, ResetsIn: "in 1d 2h", ResetsAt: "2026-08-21T14:10:00Z"},
			{Label: "session", ResetsIn: "in 3h 05m", ResetsAt: "2026-08-20T15:05:00Z"},
			{Label: "past", ResetsIn: "resetting now", ResetsAt: "2026-08-20T11:59:00Z"},
			{Label: "weekly"},
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("NewInsightsPage().Limits mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("split rows sum to the total", func(t *testing.T) {
		t.Parallel()
		resp := view.InsightsResponse{Points: []view.InsightsPoint{
			{PctWeek: 1.125, AgentPctWeek: 1, FollowUpPctWeek: 0.125},
			{PctWeek: 2, AgentPctWeek: 1.75, FollowUpPctWeek: 0.25},
		}}
		got := view.NewInsightsPage(view.Chrome{}, resp, testNow).Split
		want := view.SpendSplit{
			Rows:  []view.SplitRow{{Kind: "agent", Pct: 2.75}, {Kind: "follow-up", Pct: 0.375}},
			Total: 3.125,
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("NewInsightsPage().Split mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("merged rows run newest first in the response's timezone", func(t *testing.T) {
		t.Parallel()
		resp := view.InsightsResponse{
			Timezone: "Europe/Paris",
			Points: []view.InsightsPoint{
				{Ticket: "A", Feature: "f", MergedAt: "2026-08-19T23:30:00Z", PctWeek: 3, AgentPctWeek: 2, FollowUpPctWeek: 1},
				{Ticket: "B", Title: "Bee", MergedAt: "2026-08-20T08:00:00Z", PctWeek: 1, AgentPctWeek: 1},
			},
		}
		got := view.NewInsightsPage(view.Chrome{}, resp, testNow).Merged
		want := []view.MergedRow{
			{Name: "Bee", Day: "2026-08-20", Agent: 1, Total: 1},
			{Name: "A", Feature: "f", Day: "2026-08-20", Agent: 2, FollowUp: 1, Total: 3},
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("NewInsightsPage().Merged mismatch (-want +got):\n%s", diff)
		}
	})
}

func TestInsightsRange(t *testing.T) {
	t.Parallel()

	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatalf("time.LoadLocation(Europe/Paris): %v", err)
	}

	t.Run("since", func(t *testing.T) {
		t.Parallel()
		thirtyDaysBack := time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC)
		tests := []struct {
			name string
			raw  string
			want time.Time
		}{
			{name: "valid date", raw: "2026-08-01", want: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)},
			{name: "absent", want: thirtyDaysBack},
			{name: "unparseable", raw: "not-a-date", want: thirtyDaysBack},
			{name: "wrong format", raw: "20/26/09", want: thirtyDaysBack},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				if since, _, _ := view.InsightsRange(testNow, tt.raw); !since.Equal(tt.want) {
					t.Errorf("InsightsRange(%v, %q) since = %v, want %v", testNow, tt.raw, since, tt.want)
				}
			})
		}
	})

	t.Run("until keeps the wall-clock date at UTC midnight", func(t *testing.T) {
		t.Parallel()
		now := time.Date(2026, 6, 15, 23, 30, 0, 0, paris)
		want := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
		if _, until, _ := view.InsightsRange(now, ""); !until.Equal(want) {
			t.Errorf("InsightsRange(%v) until = %v, want %v", now, until, want)
		}
	})

	t.Run("timezone", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			loc  *time.Location
			want string
		}{
			{loc: paris, want: "Europe/Paris"},
			{loc: time.UTC, want: "UTC"},
		}
		for _, tt := range tests {
			t.Run(tt.want, func(t *testing.T) {
				t.Parallel()
				if _, _, tz := view.InsightsRange(testNow.In(tt.loc), ""); tz != tt.want {
					t.Errorf("InsightsRange(in %v) timezone = %q, want %q", tt.loc, tz, tt.want)
				}
			})
		}
	})

	t.Run("time.Local resolves to a loadable zone name", func(t *testing.T) {
		t.Parallel()
		_, _, tz := view.InsightsRange(testNow.In(time.Local), "")
		if _, err := time.LoadLocation(tz); tz == "Local" || err != nil {
			t.Errorf("InsightsRange(in Local) timezone = %q (load error %v), want an IANA name", tz, err)
		}
	})
}
