package view_test

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

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
		{name: "first non-blank line", ago: time.Minute, message: "\n  Opened the PR.\nMore.", wantAge: "1m", wantLast: "Opened the PR."},
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
			if diff := cmp.Diff(want, rows[0]); diff != "" {
				t.Errorf("ApplyActivities() row mismatch (-want +got):\n%s", diff)
			}
		})
	}

	t.Run("a ticket without activity is untouched", func(t *testing.T) {
		t.Parallel()
		rows := []view.Row{{URL: "sandbox://CC-2"}}

		view.ApplyActivities(rows, []store.Activity{{TicketURL: "sandbox://CC-1", At: testNow}}, testNow)

		if diff := cmp.Diff([]view.Row{{URL: "sandbox://CC-2"}}, rows); diff != "" {
			t.Errorf("ApplyActivities() rows mismatch (-want +got):\n%s", diff)
		}
	})
}
