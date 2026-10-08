package spend_test

import (
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/spend"
)

func TestIntervalsPairsEachReadingWithThePreviousOneForItsWindow(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 9, 28, 8, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	previous := map[agentlog.Window]agentlog.Reading{
		agentlog.FiveHour: {Window: agentlog.FiveHour, Utilization: 0.10, At: start},
	}
	readings := []agentlog.Reading{
		{Window: agentlog.FiveHour, Utilization: 0.30, At: end},
	}

	var weighedStart, weighedEnd time.Time
	weigh := func(s, e time.Time) (float64, error) {
		weighedStart, weighedEnd = s, e
		return 8, nil
	}

	intervals, latest, err := spend.Intervals(readings, previous, weigh)
	if err != nil {
		t.Fatalf("Intervals: %v", err)
	}
	if weighedStart != start || weighedEnd != end {
		t.Errorf("weigh called with (%v, %v); want (%v, %v)", weighedStart, weighedEnd, start, end)
	}

	want := spend.Interval{
		Window: agentlog.FiveHour, Start: start, End: end,
		UtilizationStart: 0.10, UtilizationEnd: 0.30, WeightUSD: 8,
	}
	if len(intervals) != 1 || intervals[0] != want {
		t.Errorf("Intervals = %+v; want [%+v]", intervals, want)
	}
	if latest[agentlog.FiveHour] != readings[0] {
		t.Errorf("latest[five_hour] = %+v; want %+v", latest[agentlog.FiveHour], readings[0])
	}
}

func TestIntervalsWithNoPreviousReadingClosesNothingYet(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 9, 28, 8, 0, 0, 0, time.UTC)
	readings := []agentlog.Reading{{Window: agentlog.SevenDay, Utilization: 0.10, At: at}}

	called := false
	weigh := func(time.Time, time.Time) (float64, error) { called = true; return 0, nil }

	intervals, latest, err := spend.Intervals(readings, nil, weigh)
	if err != nil {
		t.Fatalf("Intervals: %v", err)
	}
	if called {
		t.Error("weigh was called with no previous reading to pair against")
	}
	if len(intervals) != 0 {
		t.Errorf("Intervals = %+v; want none", intervals)
	}
	if latest[agentlog.SevenDay] != readings[0] {
		t.Errorf("latest[seven_day] = %+v; want %+v", latest[agentlog.SevenDay], readings[0])
	}
}

func TestIntervalsIgnoresADuplicateOrOlderReading(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 9, 28, 8, 0, 0, 0, time.UTC)
	previous := map[agentlog.Window]agentlog.Reading{
		agentlog.FiveHour: {Window: agentlog.FiveHour, Utilization: 0.20, At: at},
	}
	older := agentlog.Reading{Window: agentlog.FiveHour, Utilization: 0.15, At: at.Add(-time.Minute)}

	intervals, latest, err := spend.Intervals([]agentlog.Reading{older}, previous, failWeigher(t))
	if err != nil {
		t.Fatalf("Intervals: %v", err)
	}
	if len(intervals) != 0 {
		t.Errorf("Intervals = %+v; want none", intervals)
	}
	if latest[agentlog.FiveHour] != previous[agentlog.FiveHour] {
		t.Errorf("latest[five_hour] = %+v; want unchanged %+v", latest[agentlog.FiveHour], previous[agentlog.FiveHour])
	}
}

func TestIntervalsSkipsAPairStraddlingAReset(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 9, 28, 8, 0, 0, 0, time.UTC)
	previous := map[agentlog.Window]agentlog.Reading{
		agentlog.FiveHour: {Window: agentlog.FiveHour, Utilization: 0.95, At: at},
	}
	afterReset := agentlog.Reading{Window: agentlog.FiveHour, Utilization: 0.10, At: at.Add(time.Hour)}

	intervals, latest, err := spend.Intervals([]agentlog.Reading{afterReset}, previous, failWeigher(t))
	if err != nil {
		t.Fatalf("Intervals: %v", err)
	}
	if len(intervals) != 0 {
		t.Errorf("Intervals = %+v; want none across a reset", intervals)
	}
	if latest[agentlog.FiveHour] != afterReset {
		t.Errorf("latest[five_hour] = %+v; want the post-reset reading %+v", latest[agentlog.FiveHour], afterReset)
	}
}

func failWeigher(t *testing.T) spend.Weigher {
	t.Helper()
	return func(time.Time, time.Time) (float64, error) {
		t.Fatal("weigh should not have been called")
		return 0, nil
	}
}
