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

func TestWriteJSONDoesNotWriteOnEncodeFailure(t *testing.T) {
	w := httptest.NewRecorder()
	err := writeJSON(w, []any{"first", make(chan int)})
	if err == nil {
		t.Fatal("writeJSON(invalid) = nil, want encode error")
	}
	if got := w.Body.String(); got != "" {
		t.Errorf("writeJSON(invalid) body = %q, want empty", got)
	}
	if got := w.Header().Get("Content-Type"); got != "" {
		t.Errorf("writeJSON(invalid) content type = %q, want unset", got)
	}
	if got := w.Code; got != http.StatusOK {
		t.Errorf("writeJSON(invalid) status = %d, want unwritten response", got)
	}
}

func TestWriteJSONEncodeFailureAnswers500WithoutPartialBody(t *testing.T) {
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

func TestBlockedPushableFetchDoesNotBlockOtherReads(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	cache := pushableCache{list: func(context.Context) ([]gh.RepoSummary, error) {
		close(started)
		<-release
		return []gh.RepoSummary{{FullName: "first"}}, nil
	}}
	now := time.Now()
	done := make(chan error, 1)
	go func() {
		_, err := cache.get(t.Context(), now)
		done <- err
	}()
	<-started
	lockAvailable := make(chan struct{})
	go func() {
		cache.mu.Lock()
		close(lockAvailable)
		cache.mu.Unlock()
	}()
	select {
	case <-lockAvailable:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("pushable fetch held the cache mutex during I/O")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("get() = %v", err)
	}
	if repos, err := cache.get(t.Context(), now); err != nil || len(repos) != 1 || repos[0].FullName != "first" {
		t.Errorf("cached get() = %v, %v, want first repo", repos, err)
	}
}
