package web_test

import (
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
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
	board := renderPath(t, server, "/rail")

	if !strings.Contains(board, "five-hour · 42%") {
		t.Errorf("page does not show the newer five-hour reading (42%%):\n%s", board)
	}
	if !strings.Contains(board, "weekly · 0%") {
		t.Errorf("page does not show weekly at 0%% (no reading stored yet):\n%s", board)
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
	first := gaugeMarkup(t, renderPath(t, server, "/rail"))
	second := gaugeMarkup(t, renderPath(t, server, "/rail"))
	if first != second {
		t.Errorf("gauge markup changed between two polls of the same reading:\n--- first ---\n%s\n--- second ---\n%s",
			first, second)
	}
}

func TestNoticeNamesSpendLimit5hAsTheReasonSpawningPaused(t *testing.T) {
	t.Parallel()

	observedAt := testNow
	store := seededStore(t, observedAt)
	reading := agentlog.Reading{
		Window: agentlog.FiveHour, Utilization: 0.82,
		ResetsAt: observedAt.Add(time.Hour), At: observedAt,
	}
	if err := store.RecordReadings(t.Context(), []agentlog.Reading{reading}); err != nil {
		t.Fatal(err)
	}

	server := newServer(store, observedAt.Add(45*time.Second))
	server.SetSpendLimit5h(80)
	board := renderPath(t, server, "/tickets")

	if !strings.Contains(board, "spend_limit_5h") {
		t.Errorf("page does not name spend_limit_5h as the reason spawning is paused:\n%s", board)
	}
}

func TestNoticeStaysSilentBelowSpendLimit5h(t *testing.T) {
	t.Parallel()

	observedAt := testNow
	store := seededStore(t, observedAt)
	reading := agentlog.Reading{
		Window: agentlog.FiveHour, Utilization: 0.50,
		ResetsAt: observedAt.Add(time.Hour), At: observedAt,
	}
	if err := store.RecordReadings(t.Context(), []agentlog.Reading{reading}); err != nil {
		t.Fatal(err)
	}

	server := newServer(store, observedAt.Add(45*time.Second))
	server.SetSpendLimit5h(80)
	board := renderPath(t, server, "/tickets")

	if strings.Contains(board, "spend_limit_5h") {
		t.Errorf("page names spend_limit_5h though the reading is below it:\n%s", board)
	}
}

func gaugeMarkup(t *testing.T, board string) string {
	t.Helper()
	start := strings.Index(board, `<span class="meter">`)
	if start < 0 {
		t.Fatalf("no gauge markup found in rail render:\n%s", board)
	}
	end := strings.Index(board[start:], "</footer>")
	if end < 0 {
		t.Fatalf("gauge markup never closes in rail render:\n%s", board)
	}
	return board[start : start+end]
}
