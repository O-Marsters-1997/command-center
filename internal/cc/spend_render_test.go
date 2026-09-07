package cc_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
)

const spendAliveLine = `{"type":"assistant","timestamp":"2026-01-01T00:00:00.000Z","request_id":"r1",` +
	`"message":{"usage":{"input_tokens":10,"output_tokens":5}}}` + "\n"

const spendResultLine = `{"type":"result","subtype":"success","duration_ms":1000,` +
	`"num_turns":3,"total_cost_usd":1.23}` + "\n"

func spendRowStore(t *testing.T, ticket cc.Ticket, logPath string, alive bool, at time.Time) *cc.Store {
	t.Helper()

	ctx := t.Context()
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	if err := store.UpsertTickets(ctx, []cc.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}
	runID, err := store.InsertRunSkeleton(ctx, ticket.URL, "agent", "deadbeef", "hash-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordSpawn(ctx, runID, 4242, at, logPath); err != nil {
		t.Fatal(err)
	}
	obs := cc.Observation{ObservedAt: at, Runs: map[string]cc.RunObservation{ticket.URL: {Alive: alive}}}
	if err := store.SaveObservation(ctx, obs); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestBoardRendersTokensWhileARunIsAliveAndDollarsOnceItHasEnded(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

	aliveTicket := cc.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1"}
	alivePath := filepath.Join(t.TempDir(), "1.jsonl")
	if err := os.WriteFile(alivePath, []byte(spendAliveLine), 0o600); err != nil {
		t.Fatal(err)
	}
	aliveStore := spendRowStore(t, aliveTicket, alivePath, true, now)
	aliveServer := cc.NewServer(aliveStore, fixedClock(now), nil, "")
	rec := httptest.NewRecorder()
	aliveServer.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(rec.Body.String(), "15 tok") {
		t.Errorf("alive run's page does not contain \"15 tok\":\n%s", rec.Body)
	}

	endedTicket := cc.Ticket{URL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2"}
	endedPath := filepath.Join(t.TempDir(), "2.jsonl")
	if err := os.WriteFile(endedPath, []byte(spendAliveLine+spendResultLine), 0o600); err != nil {
		t.Fatal(err)
	}
	endedStore := spendRowStore(t, endedTicket, endedPath, false, now)
	endedServer := cc.NewServer(endedStore, fixedClock(now), nil, "")
	rec = httptest.NewRecorder()
	endedServer.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(rec.Body.String(), "$1.23") {
		t.Errorf("ended run's page does not contain \"$1.23\":\n%s", rec.Body)
	}
}

func TestBandSpendCardFillsFromTheSameSettledRows(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	ticket := cc.Ticket{URL: "sandbox://CC-3", Repo: "cc-sandbox", Branch: "cc-3"}
	logPath := filepath.Join(t.TempDir(), "3.jsonl")
	if err := os.WriteFile(logPath, []byte(spendAliveLine+spendResultLine), 0o600); err != nil {
		t.Fatal(err)
	}
	store := spendRowStore(t, ticket, logPath, false, now)
	server := cc.NewServer(store, fixedClock(now), nil, "")

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	body := rec.Body.String()
	for _, want := range []string{"$1.23", "1 runs", "0 failed", "avg $1.23"} {
		if !strings.Contains(body, want) {
			t.Errorf("page does not contain %q:\n%s", want, body)
		}
	}
}
