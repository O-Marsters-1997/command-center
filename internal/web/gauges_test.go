package web_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/spend"
	"github.com/O-Marsters-1997/command-center/internal/web"
)

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

	server := web.NewServer(store, fixedClock(observedAt.Add(45*time.Second)), nil, "")
	board := renderBoard(t, server)

	if !strings.Contains(board, "five-hour · 42%") {
		t.Errorf("board masthead does not show the newer five-hour reading (42%%):\n%s", board)
	}
	if !strings.Contains(board, "weekly · 0%") {
		t.Errorf("board masthead does not show weekly at 0%% (no reading stored yet):\n%s", board)
	}
}

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

	server := web.NewServer(store, fixedClock(observedAt.Add(45*time.Second)), nil, "")
	first := gaugeMarkup(t, renderBoard(t, server))
	second := gaugeMarkup(t, renderBoard(t, server))
	if first != second {
		t.Errorf("gauge markup changed between two polls of the same reading:\n--- first ---\n%s\n--- second ---\n%s",
			first, second)
	}
}

func TestMastheadGaugeSplitsIntoCCAndOtherOnceCalibrated(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	observedAt := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	store := seededStore(t, observedAt)

	server := web.NewServer(store, fixedClock(observedAt), nil, "")
	if got := renderBoard(t, server); !strings.Contains(got, "five-hour · 0% · calibrating") {
		t.Errorf("board masthead does not read calibrating below the sample threshold:\n%s", got)
	}

	projectsDir := t.TempDir()
	project := filepath.Join(projectsDir, "proj")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	var lines string
	start := observedAt.Add(-6 * time.Hour)
	for i := range spend.MinSamples {
		at := start.Add(time.Duration(i)*time.Hour + 30*time.Minute)
		lines += oneMillionInputTokensLine(at.Format(time.RFC3339), fmt.Sprintf("r%d", i)) + "\n"
	}
	if err := os.WriteFile(filepath.Join(project, "session.jsonl"), []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}

	for i := range spend.MinSamples + 1 {
		at := start.Add(time.Duration(i) * time.Hour)
		reading := agentlog.Reading{
			Window: agentlog.FiveHour, Utilization: float64(i) * 0.02,
			ResetsAt: at.Add(5 * time.Hour), At: at,
		}
		if err := store.RecordReadingsAndIntervals(ctx, []agentlog.Reading{reading}, projectsDir); err != nil {
			t.Fatalf("RecordReadingsAndIntervals (reading %d): %v", i, err)
		}
	}

	board := renderBoard(t, server)
	if !strings.Contains(board, "five-hour · 10% · 0% cc") {
		t.Errorf("board masthead does not show five-hour's cc share once calibrated:\n%s", board)
	}
}

func TestMastheadNamesSpendLimit5hAsTheReasonSpawningPaused(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	store := seededStore(t, observedAt)
	reading := agentlog.Reading{
		Window: agentlog.FiveHour, Utilization: 0.82,
		ResetsAt: observedAt.Add(time.Hour), At: observedAt,
	}
	if err := store.RecordReadings(t.Context(), []agentlog.Reading{reading}); err != nil {
		t.Fatal(err)
	}

	server := web.NewServer(store, fixedClock(observedAt.Add(45*time.Second)), nil, "")
	server.SetSpendLimit5h(80)
	board := renderBoard(t, server)

	if !strings.Contains(board, "spend_limit_5h") {
		t.Errorf("board masthead does not name spend_limit_5h as the reason spawning is paused:\n%s", board)
	}
}

func TestMastheadStaysSilentBelowSpendLimit5h(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	store := seededStore(t, observedAt)
	reading := agentlog.Reading{
		Window: agentlog.FiveHour, Utilization: 0.50,
		ResetsAt: observedAt.Add(time.Hour), At: observedAt,
	}
	if err := store.RecordReadings(t.Context(), []agentlog.Reading{reading}); err != nil {
		t.Fatal(err)
	}

	server := web.NewServer(store, fixedClock(observedAt.Add(45*time.Second)), nil, "")
	server.SetSpendLimit5h(80)
	board := renderBoard(t, server)

	if strings.Contains(board, "spend_limit_5h") {
		t.Errorf("board masthead names spend_limit_5h though the reading is below it:\n%s", board)
	}
}

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
