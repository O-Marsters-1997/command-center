package cc_test

import (
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/cc"
)

// TestMastheadRendersTheLatestStoredReading covers "the masthead gauges render the latest stored
// reading": the newest of two overlapping readings per window is what the board shows, and a
// window with nothing stored yet still renders its gauge at 0% rather than being omitted.
func TestMastheadRendersTheLatestStoredReading(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
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

	server := cc.NewServer(store, fixedClock(observedAt.Add(45*time.Second)), nil, "")
	board := renderBoard(t, server)

	if !strings.Contains(board, "five-hour · 42%") {
		t.Errorf("board masthead does not show the newer five-hour reading (42%%):\n%s", board)
	}
	if !strings.Contains(board, "weekly · 0%") {
		t.Errorf("board masthead does not show weekly at 0%% (no reading stored yet):\n%s", board)
	}
}

// TestMastheadGaugesSurviveARepeatedBoardPollWithoutFlicker covers "survive a board poll without
// flicker": with the underlying reading unchanged, two consecutive polls -- htmx's own 5s loop --
// must render byte-identical gauge markup, not just the same numbers.
func TestMastheadGaugesSurviveARepeatedBoardPollWithoutFlicker(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	store := seededStore(t, observedAt)
	reading := agentlog.Reading{
		Window: agentlog.SevenDay, Utilization: 0.19,
		ResetsAt: observedAt.Add(24 * time.Hour), At: observedAt,
	}
	if err := store.RecordReadings(t.Context(), []agentlog.Reading{reading}); err != nil {
		t.Fatal(err)
	}

	server := cc.NewServer(store, fixedClock(observedAt.Add(45*time.Second)), nil, "")
	first := gaugeMarkup(t, renderBoard(t, server))
	second := gaugeMarkup(t, renderBoard(t, server))
	if first != second {
		t.Errorf("gauge markup changed between two polls of the same reading:\n--- first ---\n%s\n--- second ---\n%s",
			first, second)
	}
}

// gaugeMarkup isolates the masthead's gauge spans out of a full board render, so the assertion
// is about their own markup rather than the rest of the poll (elapsed time, live count) that is
// expected to change tick to tick.
func gaugeMarkup(t *testing.T, board string) string {
	t.Helper()
	start := strings.Index(board, `<span class="meter">`)
	if start < 0 {
		t.Fatalf("no gauge markup found in board render:\n%s", board)
	}
	end := strings.Index(board[start:], "</div>")
	if end < 0 {
		t.Fatalf("gauge markup never closes in board render:\n%s", board)
	}
	return board[start : start+end]
}
