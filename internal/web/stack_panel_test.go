package web_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
	"github.com/O-Marsters-1997/command-center/internal/web/htmltest"
)

func stackStore(t *testing.T) *storepkg.Store {
	t.Helper()
	ctx := t.Context()
	st := openStore(t)
	base := "https://github.com/acme/web/issues/"
	tickets := []storepkg.Ticket{
		{URL: base + "1", Repo: "acme/web", Branch: "cc-1", Feature: "checkout"},
		{URL: base + "2", Repo: "acme/web", Branch: "cc-2", Feature: "checkout", BlockedBy: []string{base + "1"}},
		{URL: base + "3", Repo: "acme/web", Branch: "cc-3", Feature: "checkout", BlockedBy: []string{base + "2"}},
		{URL: base + "4", Repo: "acme/web", Branch: "cc-4", Feature: "checkout", BlockedBy: []string{base + "2"}},
	}
	storetest.Tickets(t, st, tickets...)
	if err := st.SaveObservation(ctx, plan.Observation{ObservedAt: testNow}); err != nil {
		t.Fatalf("SaveObservation: %v", err)
	}
	return st
}

func stackPanel(t *testing.T, server http.Handler, path string) htmltest.Node {
	t.Helper()
	rec := do(t, server, http.MethodGet, path, req{})
	return htmltest.Parse(t, rec.Body).One(t, ".stack-panel")
}

func testSessionStackPanel(t *testing.T) {
	t.Parallel()

	t.Run("groups the feature's tickets into waves", func(t *testing.T) {
		t.Parallel()
		panel := stackPanel(t, newServer(stackStore(t), testNow), "/s/acme/web/2")

		waves := panel.Find("section.stack-wave")
		if len(waves) != 3 {
			t.Fatalf("GET /s/acme/web/2 stack waves = %d, want 3", len(waves))
		}
		waves[0].One(t, `a[href="/s/acme/web/1"]`)
		waves[1].One(t, `a[href="/s/acme/web/2"]`)
		for _, ref := range []string{"/s/acme/web/3", "/s/acme/web/4"} {
			waves[2].One(t, `a[href="`+ref+`"]`)
		}
		if got := waves[2].One(t, "h3").Text(); got != "Wave 3" {
			t.Errorf("GET /s/acme/web/2 third wave heading = %q, want Wave 3", got)
		}
	})

	t.Run("the blockers editor sits in a closed disclosure", func(t *testing.T) {
		t.Parallel()
		panel := stackPanel(t, newServer(stackStore(t), testNow), "/s/acme/web/2")

		edit := panel.One(t, "details.stack-edit")
		if len(edit.Find("[open]")) != 0 {
			t.Errorf("GET /s/acme/web/2 Edit blockers disclosure is open, want closed")
		}
		edit.One(t, `form[action="/ticket"] input[name="blocked_by"][value="https://github.com/acme/web/issues/1"]`)
	})

	t.Run("the peek shows the feature and the ticket's props", func(t *testing.T) {
		t.Parallel()
		panel := stackPanel(t, newServer(stackStore(t), testNow), "/s/acme/web/2")

		for _, href := range []string{"/f/checkout/graph", "/f/checkout", "/s/acme/web/4"} {
			if len(panel.Find(`a[href="`+href+`"]`)) == 0 {
				t.Errorf("GET /s/acme/web/2 peek has no link to %s", href)
			}
		}
		panel.One(t, `ul.stack-list li[aria-current="page"] a[href="/s/acme/web/2"]`)
		if got := len(panel.Find(`li[aria-current="page"]`)); got != 1 {
			t.Errorf("GET /s/acme/web/2 outlined rows = %d, want 1", got)
		}
		for _, text := range []string{"checkout", "0/4 merged", "cc-2"} {
			if !strings.Contains(panel.Text(), text) {
				t.Errorf("GET /s/acme/web/2 peek = %q, want it to show %q", panel.Text(), text)
			}
		}
	})
}

func testPostTicketFromSession(t *testing.T) {
	t.Parallel()

	t.Run("the stack form returns to the session, which renders the applied edit", func(t *testing.T) {
		t.Parallel()
		st := stackStore(t)
		server := newServer(st, testNow)
		form := url.Values{
			"ticket":     {"https://github.com/acme/web/issues/3"},
			"branch":     {"cc-3"},
			"return":     {"/s/acme/web/3"},
			"blocked_by": {"https://github.com/acme/web/issues/1", ""},
		}

		rec := do(t, server, http.MethodPost, "/ticket", req{Form: form})
		if rec.Code != http.StatusSeeOther || rec.Location != "/s/acme/web/3" {
			t.Fatalf("POST /ticket = %d %q, want 303 /s/acme/web/3", rec.Code, rec.Location)
		}
		if err := st.EditTicket(t.Context(), "https://github.com/acme/web/issues/3", "cc-3",
			[]string{"https://github.com/acme/web/issues/1"}); err != nil {
			t.Fatalf("EditTicket(#3): %v", err)
		}
		waves := stackPanel(t, server, "/s/acme/web/3").Find("section.stack-wave")
		if len(waves) != 3 {
			t.Fatalf("GET /s/acme/web/3 stack waves = %d, want 3", len(waves))
		}
		waves[1].One(t, `a[href="/s/acme/web/3"]`)
		if moved := waves[2].Find(`a[href="/s/acme/web/3"]`); len(moved) != 0 {
			t.Errorf("GET /s/acme/web/3 still lists #3 in wave 3 after its blocker changed")
		}
	})

	t.Run("an off-site return goes home", func(t *testing.T) {
		t.Parallel()
		form := url.Values{
			"ticket": {"https://github.com/acme/web/issues/3"}, "branch": {"cc-3"}, "return": {"//evil.example"},
		}
		if rec := do(t, newServer(stackStore(t), testNow), http.MethodPost, "/ticket", req{Form: form}); rec.Location != "/" {
			t.Errorf("POST /ticket return=//evil.example Location = %q, want /", rec.Location)
		}
	})
}
