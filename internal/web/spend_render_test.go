package web_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

const spendAliveLine = `{"type":"assistant","timestamp":"2026-01-01T00:00:00.000Z","request_id":"r1",` +
	`"message":{"usage":{"input_tokens":10,"output_tokens":5}}}` + "\n"

const spendResultLine = `{"type":"result","subtype":"success","duration_ms":1000,` +
	`"num_turns":3,"total_cost_usd":1.23}` + "\n"

func spendRowStore(t *testing.T, ticket storepkg.Ticket, logPath string, alive bool, at time.Time) *storepkg.Store {
	t.Helper()

	ctx := t.Context()
	store := openStore(t)
	if err := store.UpsertTickets(ctx, []storepkg.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}
	runID, err := store.InsertRunSkeleton(ctx, ticket.URL, "agent", "deadbeef", "hash-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordSpawn(ctx, runID, 4242, at, logPath); err != nil {
		t.Fatal(err)
	}
	obs := plan.Observation{ObservedAt: at, Runs: map[string]plan.RunObservation{ticket.URL: {Alive: alive}}}
	if err := store.SaveObservation(ctx, obs); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestBoardRendersTokensWhileARunIsAliveAndDollarsOnceItHasEnded(t *testing.T) {
	t.Parallel()

	now := testNow

	aliveTicket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1"}
	alivePath := filepath.Join(t.TempDir(), "1.jsonl")
	if err := os.WriteFile(alivePath, []byte(spendAliveLine), 0o600); err != nil {
		t.Fatal(err)
	}
	aliveStore := spendRowStore(t, aliveTicket, alivePath, true, now)
	aliveServer := newServer(aliveStore, now)
	rec := get(t, aliveServer, "/tickets")
	if !strings.Contains(rec.Body.String(), "15 tok") {
		t.Errorf("alive run's page does not contain \"15 tok\":\n%s", rec.Body)
	}

	endedTicket := storepkg.Ticket{URL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2"}
	endedPath := filepath.Join(t.TempDir(), "2.jsonl")
	if err := os.WriteFile(endedPath, []byte(spendAliveLine+spendResultLine), 0o600); err != nil {
		t.Fatal(err)
	}
	endedStore := spendRowStore(t, endedTicket, endedPath, false, now)
	endedServer := newServer(endedStore, now)
	rec = httptest.NewRecorder()
	endedServer.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/tickets", nil))
	if !strings.Contains(rec.Body.String(), "$1.23") {
		t.Errorf("ended run's page does not contain \"$1.23\":\n%s", rec.Body)
	}
}
