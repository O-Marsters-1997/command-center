package web_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/option"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
	"github.com/O-Marsters-1997/command-center/internal/web/webtest"
)

func ticketRefs(rows []webtest.BoardRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Ref)
	}
	return out
}

func failedRootAndQueuedChildren(t *testing.T, children []string) *storepkg.Store {
	t.Helper()

	ctx := t.Context()
	store := openStore(t)
	tickets := []storepkg.Ticket{{URL: "sandbox://ROOT", Repo: "repo", Branch: "root"}}
	for _, c := range children {
		tickets = append(tickets, storepkg.Ticket{
			URL: c, Repo: "repo", Branch: strings.TrimPrefix(c, "sandbox://"),
			BlockedBy: []string{"sandbox://ROOT"},
		})
	}
	storetest.Tickets(t, store, tickets...)

	at := testNow
	runID, err := store.InsertRunSkeleton(ctx, storepkg.RunSkeleton{
		Ticket: "sandbox://ROOT", Kind: "agent", PromptHash: "hash-1",
	})
	if err != nil {
		t.Fatalf("InsertRunSkeleton(sandbox://ROOT): %v", err)
	}
	if err := store.RecordSpawn(ctx, storepkg.Spawn{
		RunID: plan.RunID(runID), Pgid: 111, StartedAt: at, LogPath: "/state/runs/1.jsonl",
	}); err != nil {
		t.Fatalf("RecordSpawn(%d): %v", runID, err)
	}
	exitCode := 1
	if err := store.RecordDisposition(ctx, storepkg.Disposition{
		RunID: plan.RunID(runID), Outcome: plan.OutcomeFailed, ExitCode: option.Some(exitCode),
		Cost: option.None[float64](), EndedAt: at,
	}); err != nil {
		t.Fatalf("RecordDisposition(%d): %v", runID, err)
	}
	for _, c := range children {
		launch := storepkg.LaunchRequest{Ticket: c, PromptHash: "hash-1", Group: "group-a", At: at}
		if err := store.QueueLaunchIntent(ctx, launch); err != nil {
			t.Fatalf("QueueLaunchIntent(%s): %v", c, err)
		}
	}
	if err := store.ApplyLaunchIntents(ctx, at); err != nil {
		t.Fatalf("ApplyLaunchIntents: %v", err)
	}
	if err := store.SaveObservation(ctx, plan.Observation{ObservedAt: at}); err != nil {
		t.Fatalf("SaveObservation: %v", err)
	}
	return store
}

func observedTickets(t *testing.T, tickets ...storepkg.Ticket) *storepkg.Store {
	t.Helper()
	store := openStore(t)
	storetest.Tickets(t, store, tickets...)
	if err := store.SaveObservation(t.Context(), plan.Observation{ObservedAt: testNow}); err != nil {
		t.Fatalf("SaveObservation: %v", err)
	}
	return store
}

func boardFor(t *testing.T, store *storepkg.Store) string {
	t.Helper()
	return renderBoard(t, openServer(track(t, store, named("repo")...), fixedClock(testNow), ""))
}

func testBoardGroups(t *testing.T) {
	t.Parallel()

	t.Run("lists a fan-out under its blocker", func(t *testing.T) {
		t.Parallel()
		children := []string{"sandbox://CC-2", "sandbox://CC-3", "sandbox://CC-4", "sandbox://CC-5"}
		page := boardFor(t, failedRootAndQueuedChildren(t, children))

		want := append([]string{ticketRef("sandbox://ROOT")}, ticketRef(children[0]), ticketRef(children[1]),
			ticketRef(children[2]), ticketRef(children[3]))
		if diff := cmp.Diff(want, ticketRefs(webtest.BoardRows(t, page))); diff != "" {
			t.Fatalf("GET /board row refs mismatch (-want +got):\n%s", diff)
		}
		if got := webtest.RowFor(t, page, "sandbox://ROOT").State; got != "failed" {
			t.Errorf("GET /board ROOT state = %q, want failed", got)
		}
		for _, c := range children {
			row := webtest.RowFor(t, page, c)
			if row.State != "queued" || !strings.Contains(row.Ticket.Text(), "sandbox://ROOT") {
				t.Errorf("GET /board %s = %q with ticket cell %q, want queued after sandbox://ROOT",
					c, row.State, row.Ticket.Text())
			}
			row.Row.One(t, `input[name="ticket"][value="`+c+`"]`)
		}
	})

	t.Run("orders children merge-first and stably", func(t *testing.T) {
		t.Parallel()
		store := failedRootAndQueuedChildren(t, []string{"sandbox://CC-5", "sandbox://CC-3", "sandbox://CC-4"})
		page := boardFor(t, store)

		want := []string{
			ticketRef("sandbox://ROOT"), ticketRef("sandbox://CC-3"),
			ticketRef("sandbox://CC-4"), ticketRef("sandbox://CC-5"),
		}
		if diff := cmp.Diff(want, ticketRefs(webtest.BoardRows(t, page))); diff != "" {
			t.Errorf("GET /board row refs mismatch (-want +got):\n%s", diff)
		}
		if again := boardFor(t, store); again != page {
			t.Errorf("GET /board twice over unchanged data differs: %v then %v",
				ticketRefs(webtest.BoardRows(t, page)), ticketRefs(webtest.BoardRows(t, again)))
		}
	})

	t.Run("puts a two-blocker row under the first only", func(t *testing.T) {
		t.Parallel()
		page := boardFor(t, observedTickets(t,
			storepkg.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"},
			storepkg.Ticket{URL: "sandbox://CC-2", Repo: "repo", Branch: "cc-2"},
			storepkg.Ticket{
				URL: "sandbox://CC-3", Repo: "repo", Branch: "cc-3",
				BlockedBy: []string{"sandbox://CC-1", "sandbox://CC-2"},
			},
		))

		want := []string{ticketRef("sandbox://CC-1"), ticketRef("sandbox://CC-2"), ticketRef("sandbox://CC-3")}
		if diff := cmp.Diff(want, ticketRefs(webtest.BoardRows(t, page))); diff != "" {
			t.Errorf("GET /board row refs mismatch (-want +got):\n%s", diff)
		}
		if got := webtest.RowFor(t, page, "sandbox://CC-3").Ticket.Text(); !strings.Contains(got, "after #CC-1, #CC-2") {
			t.Errorf("GET /board CC-3 ticket cell = %q, want after #CC-1, #CC-2", got)
		}
		for _, blocker := range []string{"sandbox://CC-1", "sandbox://CC-2"} {
			if got := webtest.RowFor(t, page, blocker).Ticket.Text(); !strings.Contains(got, "unlocks #CC-3") {
				t.Errorf("GET /board %s ticket cell = %q, want unlocks #CC-3", blocker, got)
			}
		}
	})

	t.Run("lists a chain of blockers once each", func(t *testing.T) {
		t.Parallel()
		page := boardFor(t, observedTickets(t,
			storepkg.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"},
			storepkg.Ticket{URL: "sandbox://CC-2", Repo: "repo", Branch: "cc-2", BlockedBy: []string{"sandbox://CC-1"}},
			storepkg.Ticket{URL: "sandbox://CC-3", Repo: "repo", Branch: "cc-3", BlockedBy: []string{"sandbox://CC-2"}},
		))

		want := []string{ticketRef("sandbox://CC-1"), ticketRef("sandbox://CC-2"), ticketRef("sandbox://CC-3")}
		if diff := cmp.Diff(want, ticketRefs(webtest.BoardRows(t, page))); diff != "" {
			t.Fatalf("GET /board row refs mismatch (-want +got):\n%s", diff)
		}
		if got := webtest.RowFor(t, page, "sandbox://CC-3").Ticket.Text(); !strings.Contains(got, "after #CC-2") {
			t.Errorf("GET /board CC-3 ticket cell = %q, want after #CC-2", got)
		}
	})

	t.Run("renders tickets with no blockers flat", func(t *testing.T) {
		t.Parallel()
		page := boardFor(t, observedTickets(t,
			storepkg.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"},
			storepkg.Ticket{URL: "sandbox://CC-2", Repo: "repo", Branch: "cc-2"},
		))

		want := []string{ticketRef("sandbox://CC-1"), ticketRef("sandbox://CC-2")}
		if diff := cmp.Diff(want, ticketRefs(webtest.BoardRows(t, page))); diff != "" {
			t.Errorf("GET /board row refs mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("shows a merged PR despite a later run failing", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		store := openStore(t)
		ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
		storetest.Tickets(t, store, ticket)
		runID, err := store.InsertRunSkeleton(ctx, storepkg.RunSkeleton{
			Ticket: ticket.URL, Kind: "agent", PromptHash: "hash-1",
		})
		if err != nil {
			t.Fatalf("InsertRunSkeleton(%s): %v", ticket.URL, err)
		}
		if err := store.RecordSpawn(ctx, storepkg.Spawn{
			RunID: plan.RunID(runID), Pgid: 111, StartedAt: testNow, LogPath: "/state/runs/1.jsonl",
		}); err != nil {
			t.Fatalf("RecordSpawn(%d): %v", runID, err)
		}
		if err := store.RecordDisposition(ctx, storepkg.Disposition{
			RunID: plan.RunID(runID), Outcome: plan.OutcomeFailed, ExitCode: option.Some(1),
			Cost: option.None[float64](), EndedAt: testNow,
		}); err != nil {
			t.Fatalf("RecordDisposition(%d): %v", runID, err)
		}
		obs := plan.Observation{
			ObservedAt: testNow,
			PRs:        map[plan.BranchRef]plan.PR{branchRef("repo", "cc-1"): {Number: 262, State: plan.Merged}},
		}
		if err := store.SaveObservation(ctx, obs); err != nil {
			t.Fatalf("SaveObservation: %v", err)
		}

		row := webtest.RowFor(t, boardFor(t, store), ticket.URL)
		if row.State != "merged" || row.PR.Text() != "#262 merged" {
			t.Errorf("GET /board CC-1 = %q with PR cell %q, want merged with #262 merged", row.State, row.PR.Text())
		}
	})
}
