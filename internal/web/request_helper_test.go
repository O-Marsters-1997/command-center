package web_test

import (
	"net/http"
	"net/url"
	"testing"
)

func TestDoSendsFormHeadersAndOrigin(t *testing.T) {
	t.Parallel()
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") != "http://example.com" || r.Header.Get("HX-Request") != "true" {
			t.Errorf("headers = %v, want own origin and HX-Request", r.Header)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("repo") != "acme/new" {
			t.Errorf("form repo = %q, want acme/new", r.Form.Get("repo"))
		}
		w.Header().Set("Location", "/repos/acme/new")
		w.WriteHeader(http.StatusSeeOther)
	})
	got := do(t, h, http.MethodPost, "/repos/track", req{
		Form: url.Values{"repo": {"acme/new"}}, Header: http.Header{"Hx-Request": {"true"}},
	})
	if got.Code != http.StatusSeeOther || got.Location != "/repos/acme/new" {
		t.Errorf("do = (%d, %q), want (303, /repos/acme/new)", got.Code, got.Location)
	}
}

func TestDoCanOmitOrOverrideOrigin(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ name, origin, want string }{
		{name: "missing", origin: "-", want: ""},
		{name: "foreign", origin: "https://foreign.example", want: "https://foreign.example"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.Header.Get("Origin"); got != tt.want {
					t.Errorf("Origin = %q, want %q", got, tt.want)
				}
			})
			if got := do(t, h, http.MethodGet, "/", req{Origin: tt.origin}); got.Code != http.StatusOK {
				t.Errorf("status = %d, want 200", got.Code)
			}
		})
	}
}
