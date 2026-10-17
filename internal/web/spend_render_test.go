package web_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
	"github.com/O-Marsters-1997/command-center/internal/web/webtest"
)

const spendAliveLine = `{"type":"assistant","timestamp":"2026-01-01T00:00:00.000Z","request_id":"r1",` +
	`"message":{"usage":{"input_tokens":10,"output_tokens":5}}}` + "\n"

const spendResultLine = `{"type":"result","subtype":"success","duration_ms":1000,` +
	`"num_turns":3,"total_cost_usd":1.23}` + "\n"

func spendRowStore(t *testing.T, ticket storepkg.Ticket, logPath string, alive bool, at time.Time) *storepkg.Store {
	t.Helper()

	ctx := t.Context()
	store := openStore(t)
	storetest.Tickets(t, store, ticket)
	runID, err := store.InsertRunSkeleton(ctx, storepkg.RunSkeleton{
		Ticket: ticket.URL, Kind: "agent", PromptHash: "hash-1",
	})
	if err != nil {
		t.Fatalf("InsertRunSkeleton(%q): %v", ticket.URL, err)
	}
	if err := store.RecordSpawn(ctx, storepkg.Spawn{
		RunID: plan.RunID(runID), Pgid: 4242, StartedAt: at, LogPath: logPath,
	}); err != nil {
		t.Fatalf("RecordSpawn(%q): %v", ticket.URL, err)
	}
	obs := plan.Observation{
		ObservedAt: at,
		Runs:       map[plan.TicketURL]plan.RunObservation{plan.TicketURL(ticket.URL): {Alive: alive}},
	}
	if err := store.SaveObservation(ctx, obs); err != nil {
		t.Fatalf("SaveObservation: %v", err)
	}
	return store
}

func spendCell(t *testing.T, log string, alive bool) string {
	t.Helper()
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1"}
	path := filepath.Join(t.TempDir(), "1.jsonl")
	if err := os.WriteFile(path, []byte(log), 0o600); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
	page := renderPage(t, newServer(spendRowStore(t, ticket, path, alive, testNow), testNow))
	return webtest.RowFor(t, page, ticket.URL).Row.Text()
}

func testBoardSpend(t *testing.T) {
	t.Parallel()

	t.Run("an ended run shows its tokens", func(t *testing.T) {
		t.Parallel()
		if got := spendCell(t, spendAliveLine, false); !strings.Contains(got, "15 tok") {
			t.Errorf("GET /tickets ended run spend = %q, want 15 tok", got)
		}
	})

	t.Run("an ended run never shows dollars", func(t *testing.T) {
		t.Parallel()
		if got := spendCell(t, spendAliveLine+spendResultLine, false); strings.Contains(got, "$") {
			t.Errorf("GET /tickets ended run spend = %q, want no dollar amount", got)
		}
	})
}
