package web_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cctest"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web"
)

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.OpenStore(cctest.DSN(t))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})
	return st
}

type frozenClock struct{ at time.Time }

func (c frozenClock) Now() time.Time                       { return c.at }
func (frozenClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

func fixedClock(at time.Time) loop.Clock { return frozenClock{at} }

func dispositionAsPushed(t *testing.T, st *store.Store, ticketURL string, at time.Time) {
	t.Helper()
	runID, err := st.InsertRunSkeleton(t.Context(), ticketURL, "agent", "", "hash-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.RecordSpawn(t.Context(), runID, 111, at, "/state/runs/1.jsonl"); err != nil {
		t.Fatal(err)
	}
	exitCode := 0
	if err := st.RecordDisposition(t.Context(), runID, plan.OutcomePush, &exitCode, at, nil); err != nil {
		t.Fatal(err)
	}
}

func oneMillionInputTokensLine(timestamp, requestID string) string {
	return fmt.Sprintf(
		`{"type":"assistant","timestamp":%q,"request_id":%q,`+
			`"message":{"model":"claude-sonnet-5","usage":{"input_tokens":1000000}}}`,
		timestamp, requestID,
	)
}

type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

var testNow = time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

func newServer(st *store.Store, now time.Time) *web.Server {
	return web.NewServer(st, fixedClock(now), "")
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func track(t *testing.T, st *store.Store, repos ...store.Repo) *store.Store {
	t.Helper()
	for _, repo := range repos {
		repo.State, repo.TrackedAt = store.RepoReady, testNow
		if err := st.UpsertRepo(t.Context(), repo); err != nil {
			t.Fatal(err)
		}
	}
	return st
}

func named(names ...string) []store.Repo {
	repos := make([]store.Repo, 0, len(names))
	for _, name := range names {
		repos = append(repos, store.Repo{Name: name})
	}
	return repos
}
