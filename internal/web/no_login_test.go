package web_test

import (
	"net/http"
	"testing"
)

func TestLoginAndLogoutRoutesDoNotExist(t *testing.T) {
	t.Parallel()

	server := newServer(seededStore(t, testNow), testNow)
	for _, tt := range []struct{ name, method, path string }{
		{name: "get login", method: http.MethodGet, path: "/login"},
		{name: "post login", method: http.MethodPost, path: "/login"},
		{name: "post logout", method: http.MethodPost, path: "/logout"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := do(t, server, tt.method, tt.path, req{})
			if got.Code != http.StatusNotFound {
				t.Errorf("%s %s = %d, want 404", tt.method, tt.path, got.Code)
			}
		})
	}
}

func TestPagesServeWithoutACookie(t *testing.T) {
	t.Parallel()

	got := do(t, newServer(seededStore(t, testNow), testNow), http.MethodGet, "/tickets", req{})
	if got.Code != http.StatusOK {
		t.Errorf("GET /tickets without a cookie = %d, want 200", got.Code)
	}
}
