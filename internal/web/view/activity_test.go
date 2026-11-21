package view_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	"github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

func TestBuildTray(t *testing.T) {
	t.Parallel()

	const url = "https://github.com/acme/web/issues/12"
	held := plan.IntentRecord{
		ID: 1, Ticket: url, Verb: plan.VerbReRun, QueuedBy: plan.QueuedByOwner,
		At: testNow.Add(-2 * time.Second), HoldUntil: testNow.Add(3 * time.Second),
	}
	queued := plan.IntentRecord{
		ID: 2, Ticket: url, Verb: plan.VerbRefresh, QueuedBy: plan.QueuedByLoop, At: testNow.Add(-10 * time.Minute),
	}
	done := plan.IntentRecord{
		ID: 3, Ticket: url, Verb: plan.VerbKill, At: testNow.Add(-time.Hour),
		ConsumedAt: option.Some(testNow.Add(-time.Hour)), FinishedAt: option.Some(testNow.Add(-time.Hour)),
		Outcome: plan.IntentDone,
	}

	t.Run("in progress holds queued and held intents, and Undo only while held", func(t *testing.T) {
		t.Parallel()
		tray := view.BuildTray([]plan.IntentRecord{held, queued, done}, nil, testNow)

		var got []string
		for _, item := range tray.InProgress {
			got = append(got, fmt.Sprintf("%s %s %s auto=%t undo=%t",
				item.Action, item.Ref, item.Phase, item.Auto, item.UndoUntil.IsSome()))
		}
		want := []string{"re-run web#12 held auto=false undo=true", "refresh web#12 queued auto=true undo=false"}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("BuildTray() in progress mismatch (-want +got):\n%s", diff)
		}
		if tray.Glyph != plan.GlyphPending {
			t.Errorf("BuildTray() glyph = %q, want %q", tray.Glyph, plan.GlyphPending)
		}

		after := view.BuildTray([]plan.IntentRecord{held}, nil, testNow.Add(10*time.Second))
		if got := after.InProgress[0]; got.UndoUntil.IsSome() || got.Phase != "queued" {
			t.Errorf("BuildTray() past the hold = %+v, want queued without Undo", got)
		}
	})

	t.Run("recent keeps the 8 latest steps, newest first", func(t *testing.T) {
		t.Parallel()
		var records []plan.IntentRecord
		for i := range 6 {
			rec := queued
			rec.ID = int64(i)
			rec.At = testNow.Add(-time.Duration(i) * time.Minute)
			records = append(records, rec)
		}
		records = append(records, done)

		tray := view.BuildTray(records, nil, testNow)

		if len(tray.Recent) != 8 {
			t.Errorf("BuildTray() recent = %d lines, want capped at 8", len(tray.Recent))
		}
		if !tray.Recent[0].At.Equal(testNow) {
			t.Errorf("BuildTray() first recent at %v, want the newest, %v", tray.Recent[0].At, testNow)
		}
	})

	t.Run("nothing in progress has no glyph", func(t *testing.T) {
		t.Parallel()
		tray := view.BuildTray([]plan.IntentRecord{done}, nil, testNow)
		if len(tray.InProgress) != 0 || tray.Glyph != plan.GlyphUnknown {
			t.Errorf("BuildTray() = %+v, want an empty In progress", tray)
		}
	})
}

func TestApplyActivities(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		ago      time.Duration
		message  string
		wantAge  string
		wantLast string
	}{
		{name: "under a minute", ago: 30 * time.Second, message: "Done.", wantAge: "now", wantLast: "Done."},
		{name: "minutes", ago: 2*time.Minute + 10*time.Second, message: "Done.", wantAge: "2m", wantLast: "Done."},
		{name: "hours", ago: 3 * time.Hour, wantAge: "3h"},
		{name: "days", ago: 50 * time.Hour, wantAge: "2d"},
		{
			name: "first non-blank line", ago: time.Minute, message: "\n  Opened the PR.\nMore.",
			wantAge: "1m", wantLast: "Opened the PR.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			at := testNow.Add(-tt.ago)
			rows := []view.Row{{URL: "sandbox://CC-1"}}

			view.ApplyActivities(rows, []store.Activity{{TicketURL: "sandbox://CC-1", At: at, LastMessage: tt.message}}, testNow)

			want := view.Row{
				URL: "sandbox://CC-1", Last: tt.wantLast,
				Updated: view.Age{Age: tt.wantAge, Stamp: at.Format(time.RFC3339)},
			}
			if diff := cmp.Diff(want, rows[0], equateRows); diff != "" {
				t.Errorf("ApplyActivities() row mismatch (-want +got):\n%s", diff)
			}
		})
	}

	t.Run("a ticket without activity is untouched", func(t *testing.T) {
		t.Parallel()
		rows := []view.Row{{URL: "sandbox://CC-2"}}

		view.ApplyActivities(rows, []store.Activity{{TicketURL: "sandbox://CC-1", At: testNow}}, testNow)

		if diff := cmp.Diff([]view.Row{{URL: "sandbox://CC-2"}}, rows, equateRows); diff != "" {
			t.Errorf("ApplyActivities() rows mismatch (-want +got):\n%s", diff)
		}
	})
}
