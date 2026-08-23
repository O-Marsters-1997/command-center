package cc_test

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
)

func TestServerRendersARunningRowWithPgidAndElapsed(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	task := cc.Task{TicketURL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"}
	if err := store.UpsertTasks(ctx, []cc.Task{task}); err != nil {
		t.Fatal(err)
	}

	startedAt := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	runID, err := store.InsertRunSkeleton(ctx, task.TicketURL, "agent", "deadbeef", "hash-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordSpawn(ctx, runID, 4242, startedAt, "/state/runs/1.jsonl"); err != nil {
		t.Fatal(err)
	}

	now := startedAt.Add(90 * time.Second)
	obs := cc.Observation{
		ObservedAt: now,
		Runs:       map[string]cc.RunObservation{task.TicketURL: {Alive: true}},
	}
	if err := store.SaveObservation(ctx, obs); err != nil {
		t.Fatal(err)
	}

	server := cc.NewServer(store, fixedClock(now), nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	body := rec.Body.String()
	for _, want := range []string{"running", "4242", "1m30s", "/state/runs/1.jsonl"} {
		if !strings.Contains(body, want) {
			t.Errorf("page does not contain %q:\n%s", want, body)
		}
	}
}
