package web_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoginAndLogoutRoutesDoNotExist(t *testing.T) {
	t.Parallel()

	server := newServer(seededStore(t, testNow), testNow)
	for _, req := range []struct{ method, path string }{
		{http.MethodGet, "/login"},
		{http.MethodPost, "/login"},
		{http.MethodPost, "/logout"},
	} {
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, httptest.NewRequest(req.method, req.path, nil))
		if rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s = %d, want 404", req.method, req.path, rec.Code)
		}
	}
}

func TestPagesServeWithoutACookie(t *testing.T) {
	t.Parallel()

	rec := get(t, newServer(seededStore(t, testNow), testNow), "/tickets")
	if rec.Code != http.StatusOK {
		t.Errorf("GET /tickets without a cookie = %d, want 200", rec.Code)
	}
}
