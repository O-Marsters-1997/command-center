package cc_test

import (
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

// renderedRow is one <tr> the board emitted: its attributes, which carry the grouping, and the
// ticket ref in its first cell.
type renderedRow struct {
	Attrs  string
	Ticket string
}

var rowTagRE = regexp.MustCompile(`(?s)<tr([^>]*)>\s*<td>([^<]*)</td>`)

func renderedRows(page string) []renderedRow {
	matches := rowTagRE.FindAllStringSubmatch(page, -1)
	rows := make([]renderedRow, 0, len(matches))
	for _, m := range matches {
		rows = append(rows, renderedRow{Attrs: strings.TrimSpace(m[1]), Ticket: m[2]})
	}
	return rows
}

func tickets(rows []renderedRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Ticket)
	}
	return out
}

// failedRootAndQueuedChildren seeds one root whose only run failed and count rows authorised
// behind it, which is the fan-out the PRD's "one problem, not four" is about.
func failedRootAndQueuedChildren(t *testing.T, children []string) *cc.Store {
	t.Helper()

	ctx := t.Context()
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	tasks := []cc.Task{{TicketURL: "sandbox://ROOT", Repo: "repo", Branch: "root"}}
	for _, c := range children {
		tasks = append(tasks, cc.Task{
			TicketURL: c, Repo: "repo", Branch: strings.TrimPrefix(c, "sandbox://"),
			BlockedBy: []string{"sandbox://ROOT"},
		})
	}
	if err := store.UpsertTasks(ctx, tasks); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	runID, err := store.InsertRunSkeleton(ctx, "sandbox://ROOT", "agent", "", "hash-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordSpawn(ctx, runID, 111, at, "/state/runs/1.jsonl"); err != nil {
		t.Fatal(err)
	}
	exitCode := 1
	if err := store.RecordDisposition(ctx, runID, plan.OutcomeFailed, &exitCode, at); err != nil {
		t.Fatal(err)
	}
	for _, c := range children {
		if err := store.QueueLaunchIntent(ctx, c, "hash-1", "group-a", at); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.ApplyLaunchIntents(ctx, at); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveObservation(ctx, cc.Observation{ObservedAt: at}); err != nil {
		t.Fatal(err)
	}
	return store
}

func boardFor(t *testing.T, store *cc.Store) string {
	t.Helper()

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	return renderBoard(t, cc.NewServer(store, fixedClock(at), []cc.Repo{{Name: "repo"}}, nil, ""))
}

// TestBoardRendersAFanOutAsOneGroup covers issue #74's first two acceptance criteria: four rows
// waiting on one blocker read as one failure, and every child keeps its own state and verbs.
func TestBoardRendersAFanOutAsOneGroup(t *testing.T) {
	t.Parallel()

	children := []string{"sandbox://CC-2", "sandbox://CC-3", "sandbox://CC-4", "sandbox://CC-5"}
	page := boardFor(t, failedRootAndQueuedChildren(t, children))
	rows := renderedRows(page)

	if got, want := len(rows), 5; got != want {
		t.Fatalf("rendered %d rows, want %d:\n%s", got, want, page)
	}
	if rows[0].Ticket != ticketRef("sandbox://ROOT") {
		t.Errorf("first row = %q, want the blocker's group line", rows[0].Ticket)
	}
	if !strings.Contains(rows[0].Attrs, `class="group-head"`) {
		t.Errorf("group line attrs = %q, want a group-head class", rows[0].Attrs)
	}
	if got := rowCellAt(t, page, "sandbox://ROOT", 1); got != "failed" {
		t.Errorf("group line state = %q, want the blocker's own failed", got)
	}
	for _, c := range children {
		if got := rowCellAt(t, page, c, 1); got != "queued" {
			t.Errorf("%s state = %q, want queued", c, got)
		}
		if got := rowCellAt(t, page, c, 2); !strings.Contains(got, "sandbox://ROOT") {
			t.Errorf("%s reason = %q, want it to name the blocker", c, got)
		}
		if !strings.Contains(page, `name="task" value="`+c+`"`) {
			t.Errorf("%s lost its verb buttons:\n%s", c, page)
		}
	}
	for _, r := range rows[1:] {
		if !strings.Contains(r.Attrs, `data-depth="1"`) {
			t.Errorf("child %s attrs = %q, want data-depth=1", r.Ticket, r.Attrs)
		}
	}
}

// TestBoardOrdersChildrenMergeFirstAndStably covers the third criterion: the row you merge next
// after the root is the top child, and two renders of unchanged data agree byte for byte.
func TestBoardOrdersChildrenMergeFirstAndStably(t *testing.T) {
	t.Parallel()

	store := failedRootAndQueuedChildren(t, []string{"sandbox://CC-5", "sandbox://CC-3", "sandbox://CC-4"})
	page := boardFor(t, store)

	want := []string{
		ticketRef("sandbox://ROOT"), ticketRef("sandbox://CC-3"),
		ticketRef("sandbox://CC-4"), ticketRef("sandbox://CC-5"),
	}
	if got := tickets(renderedRows(page)); !slices.Equal(got, want) {
		t.Errorf("row order = %v, want %v", got, want)
	}
	if again := boardFor(t, store); again != page {
		t.Errorf("a second render of unchanged data differs:\n--- first ---\n%s\n--- second ---\n%s", page, again)
	}
}

// TestBoardPutsATwoBlockerRowUnderTheFirstOnly covers the fourth criterion: fan-in names both
// blockers in the row's reason, but the row itself belongs to one group.
func TestBoardPutsATwoBlockerRowUnderTheFirstOnly(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	tasks := []cc.Task{
		{TicketURL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"},
		{TicketURL: "sandbox://CC-2", Repo: "repo", Branch: "cc-2"},
		{
			TicketURL: "sandbox://CC-3", Repo: "repo", Branch: "cc-3",
			BlockedBy: []string{"sandbox://CC-1", "sandbox://CC-2"},
		},
	}
	if err := store.UpsertTasks(ctx, tasks); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	if err := store.SaveObservation(ctx, cc.Observation{ObservedAt: at}); err != nil {
		t.Fatal(err)
	}

	page := boardFor(t, store)
	want := []string{ticketRef("sandbox://CC-1"), ticketRef("sandbox://CC-3"), ticketRef("sandbox://CC-2")}
	if got := tickets(renderedRows(page)); !slices.Equal(got, want) {
		t.Errorf("row order = %v, want CC-3 under CC-1 only, %v", got, want)
	}
	if got := rowCellAt(t, page, "sandbox://CC-3", 2); !strings.Contains(got, "sandbox://CC-2") {
		t.Errorf("CC-3 reason = %q, want it to still name both blockers", got)
	}
}

// TestBoardRendersATaskSetWithNoBlockersFlat covers the rest of the fourth criterion: nothing is
// indented and nothing carries a group line when no row waits on another.
func TestBoardRendersATaskSetWithNoBlockersFlat(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	tasks := []cc.Task{
		{TicketURL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"},
		{TicketURL: "sandbox://CC-2", Repo: "repo", Branch: "cc-2"},
	}
	if err := store.UpsertTasks(ctx, tasks); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	if err := store.SaveObservation(ctx, cc.Observation{ObservedAt: at}); err != nil {
		t.Fatal(err)
	}

	page := boardFor(t, store)
	if strings.Contains(page, `class="group-head"`) {
		t.Errorf("a blockerless task set rendered a group line:\n%s", page)
	}
	for _, r := range renderedRows(page) {
		if !strings.Contains(r.Attrs, `data-depth="0"`) {
			t.Errorf("%s attrs = %q, want data-depth=0", r.Ticket, r.Attrs)
		}
	}
}

const goldenGroupedBoard = "testdata/board_grouped.golden.html"

// TestBoardGoldensAFiveRowFanOutPlusAnUngroupedRow covers issue #74's fifth acceptance criterion:
// a golden covering a five-row fan-out plus an ungrouped row.
func TestBoardGoldensAFiveRowFanOutPlusAnUngroupedRow(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := failedRootAndQueuedChildren(t,
		[]string{"sandbox://CC-2", "sandbox://CC-3", "sandbox://CC-4", "sandbox://CC-5"})
	if err := store.UpsertTasks(ctx, []cc.Task{{TicketURL: "sandbox://LONE", Repo: "repo", Branch: "lone"}}); err != nil {
		t.Fatal(err)
	}

	assertGolden(t, goldenGroupedBoard, []byte(boardFor(t, store)))
}
