package web

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/gh"
)

var frameTestNow = time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

func TestWriteJSONEncodeFailureAnswers500WithoutPartialBody(t *testing.T) {
	t.Parallel()

	s := &Server{logger: slog.New(slog.DiscardHandler)}
	h := s.handle(func(w http.ResponseWriter, _ *http.Request) error {
		return writeJSON(w, []any{"first", make(chan int)})
	})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/graph.json", nil))
	if got := w.Code; got != http.StatusInternalServerError {
		t.Errorf("GET /graph.json on encode error = %d, want 500", got)
	}
	if got := w.Body.String(); got != "internal error\n" {
		t.Errorf("GET /graph.json on encode error body = %q, want only internal error", got)
	}
}

func TestPushableFetchSharesResultAcrossConcurrentRequests(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	release := make(chan struct{})
	calls := 0
	cache := pushableCache{list: func(context.Context) ([]gh.RepoSummary, error) {
		calls++
		close(started)
		<-release
		return []gh.RepoSummary{{FullName: "first"}}, nil
	}}
	done := make(chan error, 2)
	go func() {
		_, err := cache.get(t.Context(), frameTestNow)
		done <- err
	}()
	<-started
	go func() {
		_, err := cache.get(t.Context(), frameTestNow)
		done <- err
	}()
	close(release)
	for range 2 {
		if err := <-done; err != nil {
			t.Errorf("get() = %v, want nil", err)
		}
	}
	if calls != 1 {
		t.Errorf("list calls = %d, want 1", calls)
	}
}
