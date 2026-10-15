package web_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/web/webtest"

	"github.com/O-Marsters-1997/command-center/internal/option"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
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

func boardFor(t *testing.T, store *storepkg.Store) string {
	t.Helper()

	at := testNow
	return renderBoard(t, openServer(track(t, store, named("repo")...), fixedClock(at), ""))
}

func TestBoardListsAFanOutBySection(t *testing.T) {
	t.Parallel()

	children := []string{"sandbox://CC-2", "sandbox://CC-3", "sandbox://CC-4", "sandbox://CC-5"}
	page := boardFor(t, failedRootAndQueuedChildren(t, children))
	rows := webtest.BoardRows(t, page)

	if got, want := len(rows), 5; got != want {
		t.Fatalf("rendered %d rows, want %d; refs = %v", got, want, ticketRefs(rows))
	}
	if rows[0].Ref != ticketRef("sandbox://ROOT") {
		t.Errorf("first row = %q, want the blocker's group line:\n%s", rows[0].Ref, rows[0].Row.HTML())
	}
	root := webtest.RowFor(t, page, "sandbox://ROOT")
	if got := root.State; got != "failed" {
		t.Errorf("group line state = %q, want failed:\n%s", got, root.Row.HTML())
	}
	for _, c := range children {
		row := webtest.RowFor(t, page, c)
		if got := row.State; got != "queued" {
			t.Errorf("%s state = %q, want queued:\n%s", c, got, row.Row.HTML())
		}
		if got := row.Ticket.Text(); !strings.Contains(got, "sandbox://ROOT") {
			t.Errorf("%s reason = %q, want blocker:\n%s", c, got, row.Row.HTML())
		}
		row.Row.One(t, `input[name="ticket"][value="`+c+`"]`)
	}
}

func TestBoardOrdersChildrenMergeFirstAndStably(t *testing.T) {
	t.Parallel()

	store := failedRootAndQueuedChildren(t, []string{"sandbox://CC-5", "sandbox://CC-3", "sandbox://CC-4"})
	page := boardFor(t, store)

	want := []string{
		ticketRef("sandbox://ROOT"), ticketRef("sandbox://CC-3"),
		ticketRef("sandbox://CC-4"), ticketRef("sandbox://CC-5"),
	}
	if got := ticketRefs(webtest.BoardRows(t, page)); !slices.Equal(got, want) {
		t.Errorf("row order = %v, want %v; first row:\n%s", got, want, webtest.BoardRows(t, page)[0].Row.HTML())
	}
	if again := boardFor(t, store); again != page {
		t.Errorf("a second render of unchanged data differs; first refs = %v, second refs = %v",
			ticketRefs(webtest.BoardRows(t, page)), ticketRefs(webtest.BoardRows(t, again)))
	}
}

func TestBoardPutsATwoBlockerRowUnderTheFirstOnly(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	tickets := []storepkg.Ticket{
		{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"},
		{URL: "sandbox://CC-2", Repo: "repo", Branch: "cc-2"},
		{
			URL: "sandbox://CC-3", Repo: "repo", Branch: "cc-3",
			BlockedBy: []string{"sandbox://CC-1", "sandbox://CC-2"},
		},
	}
	storetest.Tickets(t, store, tickets...)
	at := testNow
	if err := store.SaveObservation(ctx, plan.Observation{ObservedAt: at}); err != nil {
		t.Fatalf("SaveObservation: %v", err)
	}

	page := boardFor(t, store)
	want := []string{ticketRef("sandbox://CC-1"), ticketRef("sandbox://CC-2"), ticketRef("sandbox://CC-3")}
	if got := ticketRefs(webtest.BoardRows(t, page)); !slices.Equal(got, want) {
		t.Errorf("row order = %v, want %v; child row:\n%s", got, want,
			webtest.RowFor(t, page, "sandbox://CC-3").Row.HTML())
	}
	child := webtest.RowFor(t, page, "sandbox://CC-3")
	if got := child.Ticket.Text(); !strings.Contains(got, "after #CC-1, #CC-2") {
		t.Errorf("CC-3 ticket cell = %q, want both blockers:\n%s", got, child.Row.HTML())
	}
	for _, blocker := range []string{"sandbox://CC-1", "sandbox://CC-2"} {
		row := webtest.RowFor(t, page, blocker)
		if got := row.Ticket.Text(); !strings.Contains(got, "unlocks #CC-3") {
			t.Errorf("%s ticket cell = %q, want unlocks #CC-3:\n%s", blocker, got, row.Row.HTML())
		}
	}
}

func TestBoardListsAChainOfBlockersOnceEach(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	tickets := []storepkg.Ticket{
		{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"},
		{URL: "sandbox://CC-2", Repo: "repo", Branch: "cc-2", BlockedBy: []string{"sandbox://CC-1"}},
		{URL: "sandbox://CC-3", Repo: "repo", Branch: "cc-3", BlockedBy: []string{"sandbox://CC-2"}},
	}
	storetest.Tickets(t, store, tickets...)
	at := testNow
	if err := store.SaveObservation(ctx, plan.Observation{ObservedAt: at}); err != nil {
		t.Fatalf("SaveObservation: %v", err)
	}

	page := boardFor(t, store)
	rows := webtest.BoardRows(t, page)
	want := []string{ticketRef("sandbox://CC-1"), ticketRef("sandbox://CC-2"), ticketRef("sandbox://CC-3")}
	if got := ticketRefs(rows); !slices.Equal(got, want) {
		t.Fatalf("row order = %v, want %v; child row:\n%s", got, want,
			webtest.RowFor(t, page, "sandbox://CC-3").Row.HTML())
	}
	child := webtest.RowFor(t, page, "sandbox://CC-3")
	if got := child.Ticket.Text(); !strings.Contains(got, "after #CC-2") {
		t.Errorf("CC-3 ticket cell = %q, want direct blocker:\n%s", got, child.Row.HTML())
	}
}

func TestBoardRendersATicketSetWithNoBlockersFlat(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	tickets := []storepkg.Ticket{
		{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"},
		{URL: "sandbox://CC-2", Repo: "repo", Branch: "cc-2"},
	}
	storetest.Tickets(t, store, tickets...)
	at := testNow
	if err := store.SaveObservation(ctx, plan.Observation{ObservedAt: at}); err != nil {
		t.Fatalf("SaveObservation: %v", err)
	}

	page := boardFor(t, store)
	rows := webtest.BoardRows(t, page)
	if got, want := len(rows), len(tickets); got != want {
		t.Errorf("rendered %d rows for %d tickets, want no extra group line; refs = %v",
			got, want, ticketRefs(rows))
	}
}

func TestBoardShowsAMergedPRDespiteALaterRunFailing(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	storetest.Tickets(t, store, ticket)

	at := testNow
	runID, err := store.InsertRunSkeleton(ctx, storepkg.RunSkeleton{
		Ticket: ticket.URL, Kind: "agent", PromptHash: "hash-1",
	})
	if err != nil {
		t.Fatalf("InsertRunSkeleton(%s): %v", ticket.URL, err)
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

	obs := plan.Observation{
		ObservedAt: at,
		PRs:        map[plan.BranchRef]plan.PR{branchRef("repo", "cc-1"): {Number: 262, State: plan.Merged}},
	}
	if err := store.SaveObservation(ctx, obs); err != nil {
		t.Fatalf("SaveObservation: %v", err)
	}

	page := boardFor(t, store)
	row := webtest.RowFor(t, page, ticket.URL)
	if got := row.State; got != "merged" {
		t.Errorf("state = %q, want merged despite the failed run:\n%s", got, row.Row.HTML())
	}
	if got := row.PR.Text(); got != "#262 merged" {
		t.Errorf("pr cell = %q, want #262 merged:\n%s", got, row.Row.HTML())
	}
}
