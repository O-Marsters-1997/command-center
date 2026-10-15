package view

import (
	"os"
	"testing"
	"time"
)

func TestSystemTimezoneNameReadsEtcLocaltime(t *testing.T) {
	t.Parallel()

	if _, err := os.Readlink("/etc/localtime"); err != nil {
		t.Skip("no /etc/localtime symlink on this platform")
	}
	if got := systemTimezoneName(); got == "" {
		t.Error(`systemTimezoneName() = "", want a name resolved from /etc/localtime`)
	}
}

func TestInsightsTimezoneUsesTheLocationsOwnNameWhenItHasOne(t *testing.T) {
	t.Parallel()

	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatalf("time.LoadLocation(Europe/Paris): %v", err)
	}
	if got := insightsTimezone(paris); got != "Europe/Paris" {
		t.Errorf("insightsTimezone(Europe/Paris) = %q, want Europe/Paris", got)
	}
	if got := insightsTimezone(time.UTC); got != "UTC" {
		t.Errorf("insightsTimezone(UTC) = %q, want UTC", got)
	}
}

func TestInsightsTimezoneResolvesTimeLocalFromTheOSRatherThanUTC(t *testing.T) {
	t.Parallel()

	want := systemTimezoneName()
	if want == "" {
		want = "UTC"
	}
	if got := insightsTimezone(time.Local); got != want {
		t.Errorf("insightsTimezone(time.Local) = %q, want %q", got, want)
	}
}

func TestCivilDateKeepsTheWallClockDateAtUTCMidnight(t *testing.T) {
	t.Parallel()

	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatalf("time.LoadLocation(Europe/Paris): %v", err)
	}
	got := civilDate(time.Date(2026, 6, 15, 23, 30, 0, 0, paris))
	want := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("civilDate(...) = %v, want %v", got, want)
	}
}

func TestParseSinceOrDefault(t *testing.T) {
	t.Parallel()

	until := testNow
	for _, tc := range []struct {
		name string
		raw  string
		want time.Time
	}{
		{name: "valid date", raw: "2026-08-01", want: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)},
		{name: "absent", raw: "", want: until.AddDate(0, 0, -insightsDefaultRangeDays)},
		{name: "unparseable", raw: "not-a-date", want: until.AddDate(0, 0, -insightsDefaultRangeDays)},
		{name: "wrong format", raw: "20/26/09", want: until.AddDate(0, 0, -insightsDefaultRangeDays)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := parseSinceOrDefault(tc.raw, until); !got.Equal(tc.want) {
				t.Errorf("parseSinceOrDefault(%q, %v) = %v, want %v", tc.raw, until, got, tc.want)
			}
		})
	}
}

func TestSpendSplitRowsSumToTheChartTotal(t *testing.T) {
	t.Parallel()

	points := []insightsPointJSON{
		{PctWeek: 1.125, AgentPctWeek: 1, FollowUpPctWeek: 0.125},
		{PctWeek: 2, AgentPctWeek: 1.75, FollowUpPctWeek: 0.25},
	}
	split := buildSpendSplit(points)
	var sum float64
	for _, row := range split.Rows {
		sum += row.Pct
	}
	if sum != split.Total || split.Total != 3.125 {
		t.Errorf("rows sum to %v, total %v, want both 3.125", sum, split.Total)
	}
}

func TestBuildLimitsLeavesAWindowWithoutAReadingBlank(t *testing.T) {
	t.Parallel()

	now := testNow
	limits := buildLimits([]Gauge{
		{Label: "five-hour", ResetsAt: now.Add(26*time.Hour + 10*time.Minute)},
		{Label: "weekly"},
	}, now)
	if limits[0].ResetsIn != "in 1d 2h" {
		t.Errorf("ResetsIn = %q, want in 1d 2h", limits[0].ResetsIn)
	}
	if limits[1].ResetsIn != "" {
		t.Errorf("ResetsIn = %q, want empty", limits[1].ResetsIn)
	}
}
