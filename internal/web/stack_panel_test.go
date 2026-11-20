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

	t.Run("folds the feature's idle tickets behind counts", func(t *testing.T) {
		t.Parallel()
		panel := stackPanel(t, newServer(stackStore(t), testNow), "/s/acme/web/2")

		live := panel.One(t, `section[aria-labelledby="stack-live"]`)
		if !strings.Contains(live.Text(), "Nothing is running") {
			t.Errorf("GET /s/acme/web/2 In flight = %q, want it to say nothing is running", live.Text())
		}
		folds := live.Find("details")
		if len(folds) != 2 {
			t.Fatalf("GET /s/acme/web/2 folds = %d, want 2 (Ready to launch, Blocked)", len(folds))
		}
		for i, want := range []struct {
			label string
			count string
			hrefs []string
		}{
			{"Ready to launch", "1", []string{"/s/acme/web/1"}},
			{"Blocked", "3", []string{"/s/acme/web/2", "/s/acme/web/3", "/s/acme/web/4"}},
		} {
			if _, open := folds[i].Attr("open"); open {
				t.Errorf("GET /s/acme/web/2 fold %q is open, want closed", want.label)
			}
			summary := folds[i].One(t, "summary")
			if !strings.Contains(summary.Text(), want.label) {
				t.Errorf("GET /s/acme/web/2 fold %d summary = %q, want %q", i, summary.Text(), want.label)
			}
			if got := summary.One(t, ".stack-count").Text(); got != want.count {
				t.Errorf("GET /s/acme/web/2 %s count = %q, want %q", want.label, got, want.count)
			}
			for _, href := range want.hrefs {
				folds[i].One(t, `a[href="`+href+`"]`)
			}
		}
	})

	t.Run("this ticket shows what blocks it and what it unlocks", func(t *testing.T) {
		t.Parallel()
		panel := stackPanel(t, newServer(stackStore(t), testNow), "/s/acme/web/2")

		this := panel.One(t, `section[aria-labelledby="stack-this"]`)
		for _, href := range []string{"/s/acme/web/1", "/s/acme/web/3", "/s/acme/web/4"} {
			this.One(t, `a[href="`+href+`"][hx-select="#main"]`)
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
		panel.One(t, `a[aria-current="page"][href="/s/acme/web/2"]`)
		if got := len(panel.Find(`[aria-current="page"]`)); got != 1 {
			t.Errorf("GET /s/acme/web/2 current rows = %d, want 1", got)
		}
		panel.One(t, `[role="meter"][aria-valuenow="0"]`)
		for _, text := range []string{"checkout", "0/4 merged", "Checks"} {
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
		live := stackPanel(t, server, "/s/acme/web/3").One(t, `section[aria-labelledby="stack-live"]`)
		if got := live.One(t, `a[href="/s/acme/web/3"]`).Text(); !strings.Contains(got, "wave 2") {
			t.Errorf("GET /s/acme/web/3 own row = %q, want it in wave 2 after its blocker changed", got)
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
