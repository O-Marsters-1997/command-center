package web

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteJSON(t *testing.T) {
	t.Parallel()

	unencodable := []any{"first", make(chan int)}

	t.Run("writes nothing on an encode failure", func(t *testing.T) {
		t.Parallel()
		w := httptest.NewRecorder()
		if err := writeJSON(w, unencodable); err == nil {
			t.Fatal("writeJSON(unencodable) = nil, want encode error")
		}
		if got := w.Body.String(); got != "" {
			t.Errorf("writeJSON(unencodable) body = %q, want empty", got)
		}
		if got := w.Header().Get("Content-Type"); got != "" {
			t.Errorf("writeJSON(unencodable) content type = %q, want unset", got)
		}
		if got := w.Code; got != http.StatusOK {
			t.Errorf("writeJSON(unencodable) status = %d, want unwritten response", got)
		}
	})

	t.Run("answers 500 without a partial body through the handler", func(t *testing.T) {
		t.Parallel()
		s := &Server{logger: slog.New(slog.DiscardHandler)}
		h := s.handle(func(w http.ResponseWriter, _ *http.Request) error {
			return writeJSON(w, unencodable)
		})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/graph.json", nil))
		if got := w.Code; got != http.StatusInternalServerError {
			t.Errorf("GET /graph.json on encode error = %d, want 500", got)
		}
		if got := w.Body.String(); got != "internal error\n" {
			t.Errorf("GET /graph.json on encode error body = %q, want only internal error", got)
		}
	})
}
