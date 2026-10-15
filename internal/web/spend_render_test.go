package web_test

import (
	"net/http"
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
		Ticket: ticket.URL, Kind: "agent", BaselineSHA: "deadbeef", PromptHash: "hash-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordSpawn(ctx, storepkg.Spawn{
		RunID: plan.RunID(runID), Pgid: 4242, StartedAt: at, LogPath: logPath,
	}); err != nil {
		t.Fatal(err)
	}
	obs := plan.Observation{
		ObservedAt: at,
		Runs:       map[plan.TicketURL]plan.RunObservation{plan.TicketURL(ticket.URL): {Alive: alive}},
	}
	if err := store.SaveObservation(ctx, obs); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestBoardRendersTokensAndNeverDollars(t *testing.T) {
	t.Parallel()

	now := testNow

	aliveTicket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1"}
	alivePath := filepath.Join(t.TempDir(), "1.jsonl")
	if err := os.WriteFile(alivePath, []byte(spendAliveLine), 0o600); err != nil {
		t.Fatal(err)
	}
	aliveStore := spendRowStore(t, aliveTicket, alivePath, true, now)
	aliveServer := newServer(aliveStore, now)
	got := do(t, aliveServer, http.MethodGet, "/tickets", req{})
	aliveSpend := webtest.RowFor(t, got.Body, aliveTicket.URL).Spend
	if text := aliveSpend.Text(); !strings.Contains(text, "15 tok") {
		t.Errorf("alive run spend = %q, want 15 tok", text)
	}

	endedTicket := storepkg.Ticket{URL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2"}
	endedPath := filepath.Join(t.TempDir(), "2.jsonl")
	if err := os.WriteFile(endedPath, []byte(spendAliveLine+spendResultLine), 0o600); err != nil {
		t.Fatal(err)
	}
	endedStore := spendRowStore(t, endedTicket, endedPath, false, now)
	endedServer := newServer(endedStore, now)
	got = do(t, endedServer, http.MethodGet, "/tickets", req{})
	endedSpend := webtest.RowFor(t, got.Body, endedTicket.URL).Spend
	if strings.Contains(endedSpend.Text(), "$1.23") {
		t.Errorf("ended run spend contains dollar amount: %q", endedSpend.Text())
	}
}
