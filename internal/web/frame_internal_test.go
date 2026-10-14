package web

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cctest"
	"github.com/O-Marsters-1997/command-center/internal/gh"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

type boardTestClock struct{}

func (boardTestClock) Now() time.Time { return time.Now() }

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

func TestBoardRenderDoesNotWaitOnPushableFetch(t *testing.T) {
	st, err := store.OpenStore(t.Context(), cctest.DSN(t))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	started := make(chan struct{})
	release := make(chan struct{})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	s := NewServer(Deps{Logger: slog.New(slog.DiscardHandler), Store: st, Clock: boardTestClock{}},
		WithPushableSource(func(ctx context.Context) ([]gh.RepoSummary, error) {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return []gh.RepoSummary{{FullName: "first"}}, nil
		}))
	fetchDone := make(chan error, 1)
	go func() {
		_, err := s.pushable.get(ctx, time.Now())
		fetchDone <- err
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatalf("pushable fetch did not start: %v", ctx.Err())
	}

	response := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/board", nil).WithContext(ctx)
		req.Header.Set("HX-Request", "true")
		s.ServeHTTP(w, req)
		response <- w
	}()
	timedOut := false
	select {
	case w := <-response:
		if got := w.Code; got != http.StatusOK {
			t.Errorf("GET /board during pushable fetch = %d, want 200", got)
		}
		if !strings.Contains(w.Body.String(), `id="board"`) {
			t.Error("GET /board during pushable fetch body has no board")
		}
	case <-time.After(3 * time.Second):
		t.Error("GET /board did not complete while pushable fetch was blocked")
		timedOut = true
	}
	close(release)
	select {
	case err := <-fetchDone:
		if err != nil {
			t.Errorf("pushable fetch = %v, want nil", err)
		}
	case <-ctx.Done():
		t.Errorf("pushable fetch did not finish: %v", ctx.Err())
	}
	if timedOut {
		select {
		case <-response:
		case <-ctx.Done():
			t.Errorf("GET /board did not finish after fetch release: %v", ctx.Err())
		}
	}
}
