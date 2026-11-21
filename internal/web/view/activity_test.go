package view_test

import (
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	"github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

func TestParseActivityParams(t *testing.T) {
	t.Parallel()

	t.Run("every field round-trips through the query", func(t *testing.T) {
		t.Parallel()
		want := view.ActivityParams{
			Query: "boom", Outcome: plan.IntentFailed, Verb: plan.VerbReRun,
			Project: "core", Feature: "feat-a", Range: view.Range7d, Page: 3,
		}
		q, err := url.ParseQuery(want.Encode())
		if err != nil {
			t.Fatalf("ParseQuery(%q): %v", want.Encode(), err)
		}

		if diff := cmp.Diff(want, view.ParseActivityParams(q, view.Params{})); diff != "" {
			t.Errorf("ParseActivityParams(Encode()) mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("project and feature default to the rail scope until the query names them", func(t *testing.T) {
		t.Parallel()
		rail := view.Params{Project: "core", Feature: "feat-a"}

		got := view.ParseActivityParams(url.Values{}, rail)
		if got.Project != "core" || got.Feature != "feat-a" {
			t.Errorf("absent scope = %q/%q, want the rail's core/feat-a", got.Project, got.Feature)
		}
		got = view.ParseActivityParams(url.Values{"project": {""}}, rail)
		if got.Project != "" {
			t.Errorf("explicitly empty project = %q, want all projects", got.Project)
		}
	})

	t.Run("junk values fall back to defaults", func(t *testing.T) {
		t.Parallel()
		q := url.Values{"outcome": {"bogus"}, "verb": {"bogus"}, "range": {"1y"}, "page": {"-4"}}

		want := view.ActivityParams{Page: 1}
		if diff := cmp.Diff(want, view.ParseActivityParams(q, view.Params{})); diff != "" {
			t.Errorf("ParseActivityParams() mismatch (-want +got):\n%s", diff)
		}
	})
}

func TestNewActivityPage(t *testing.T) {
	t.Parallel()

	row := func(id int64, ago time.Duration, outcome plan.IntentOutcome, title string) view.ActivityRow {
		return view.ActivityRow{
			ID: id, Queued: testNow.Add(-ago), Action: string(plan.VerbReRun), Ref: "repo#1", Title: title, Outcome: outcome,
		}
	}
	rows := []view.ActivityRow{
		row(4, time.Hour, plan.IntentFailed, "refused thing"),
		row(3, 2*time.Hour, plan.IntentDone, "done thing"),
		row(2, 3*24*time.Hour, plan.IntentWithdrawn, "old thing"),
		row(1, 20*24*time.Hour, plan.IntentDone, "ancient thing"),
	}

	t.Run("counts ignore the outcome tab but respect the other filters", func(t *testing.T) {
		t.Parallel()
		p := view.ActivityParams{Outcome: plan.IntentFailed, Range: view.Range7d, Page: 1}

		page := view.NewActivityPage(view.Chrome{}, p, rows, nil, testNow)

		want := map[plan.IntentOutcome]int{plan.IntentFailed: 1, plan.IntentDone: 1, plan.IntentWithdrawn: 1}
		if diff := cmp.Diff(want, page.Counts); diff != "" {
			t.Errorf("Counts mismatch (-want +got):\n%s", diff)
		}
		if page.Total != 1 || len(page.Days) != 1 || page.Days[0].Rows[0].ID != 4 {
			t.Errorf("Total = %d, Days = %+v, want only the failed row", page.Total, page.Days)
		}
	})

	t.Run("search matches the title case-insensitively", func(t *testing.T) {
		t.Parallel()

		page := view.NewActivityPage(view.Chrome{}, view.ActivityParams{Query: "ANCIENT", Page: 1}, rows, nil, testNow)

		if page.Total != 1 || page.Days[0].Rows[0].ID != 1 {
			t.Errorf("Total = %d, Days = %+v, want only the ancient row", page.Total, page.Days)
		}
	})

	t.Run("a page past the end clamps to the last", func(t *testing.T) {
		t.Parallel()
		many := make([]view.ActivityRow, 30)
		for i := range many {
			many[i] = row(int64(30-i), time.Duration(i)*time.Minute, plan.IntentDone, "t")
		}

		page := view.NewActivityPage(view.Chrome{}, view.ActivityParams{Page: 9}, many, nil, testNow)

		if page.Page != 2 || page.Pages != 2 || page.Params.Page != 2 || page.Last() != 30 || page.First() != 26 {
			t.Errorf("page %d of %d shows %d-%d, want page 2 of 2 showing 26-30",
				page.Page, page.Pages, page.First(), page.Last())
		}
		if page.OlderPath() != "" || page.NewerPath() != "/activity" {
			t.Errorf("paths = %q/%q, want no older and newer back to /activity", page.OlderPath(), page.NewerPath())
		}
	})

	t.Run("empty states name the filters", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name string
			rows []view.ActivityRow
			p    view.ActivityParams
			want string
		}{
			{name: "no history", p: view.ActivityParams{Page: 1}, want: "No actions in the last 30 days."},
			{
				name: "filtered",
				rows: rows,
				p:    view.ActivityParams{Outcome: plan.IntentFailed, Query: "zzzz", Range: view.RangeToday, Page: 1},
				want: "No failed actions match 'zzzz' today.",
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				if got := view.NewActivityPage(view.Chrome{}, tt.p, tt.rows, nil, testNow).Empty; got != tt.want {
					t.Errorf("Empty = %q, want %q", got, tt.want)
				}
			})
		}
	})
}

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
