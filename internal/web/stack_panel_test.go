package web_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/htmltest"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
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

func TestSessionStackPanelListsBlockersAndUnlocks(t *testing.T) {
	t.Parallel()
	page := htmltest.Parse(t, renderPath(t, newServer(stackStore(t), testNow), "/s/acme/web/2"))
	panel := page.One(t, ".stack-panel")
	lists := panel.Find("ul.stack-list")
	if len(lists) != 3 {
		t.Fatalf("stack lists = %d, want 3", len(lists))
	}
	lists[1].One(t, `a[href="/s/acme/web/1"]`)
	for _, ref := range []string{"/s/acme/web/3", "/s/acme/web/4"} {
		lists[2].One(t, `a[href="`+ref+`"]`)
	}
	panel.One(t, `input[name="blocked_by"][value="https://github.com/acme/web/issues/1"]`)
}

func TestSessionStackFormReturnsToTheSessionAndRendersTheAppliedEdit(t *testing.T) {
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
		t.Fatalf("status = %d, location = %q", rec.Code, rec.Location)
	}
	if err := st.EditTicket(t.Context(), "https://github.com/acme/web/issues/3", "cc-3",
		[]string{"https://github.com/acme/web/issues/1"}); err != nil {
		t.Fatalf("EditTicket(%q): %v", "https://github.com/acme/web/issues/3", err)
	}
	page := htmltest.Parse(t, renderPath(t, server, "/s/acme/web/3"))
	panel := page.One(t, ".stack-panel")
	lists := panel.Find("ul.stack-list")
	if len(lists) != 2 {
		t.Fatalf("stack lists = %d, want 2", len(lists))
	}
	lists[1].One(t, `a[href="/s/acme/web/1"]`)
	if old := lists[1].Find(`a[href="/s/acme/web/2"]`); len(old) != 0 {
		t.Errorf("panel still has the former blocker: %s", old[0].HTML())
	}
}

func TestSessionPeekPanelShowsTheFeatureAndTheTicketsProps(t *testing.T) {
	t.Parallel()
	page := htmltest.Parse(t, renderPath(t, newServer(stackStore(t), testNow), "/s/acme/web/2"))
	panel := page.One(t, ".stack-panel")
	for _, href := range []string{"/f/checkout/graph", "/f/checkout"} {
		panel.One(t, `a[href="`+href+`"]`)
	}
	panel.One(t, `ul.stack-list li[aria-current="page"] a[href="/s/acme/web/2"]`)
	if links := panel.Find(`a[href="/s/acme/web/4"]`); len(links) == 0 {
		t.Error("stack has no #4 link")
	}
	for _, text := range []string{"checkout", "0/4 merged", "cc-2"} {
		if !strings.Contains(panel.Text(), text) {
			t.Errorf("peek panel has no property %q", text)
		}
	}
}

func TestTicketEditIgnoresAnOffSiteReturn(t *testing.T) {
	t.Parallel()
	server := newServer(stackStore(t), testNow)
	form := url.Values{
		"ticket": {"https://github.com/acme/web/issues/3"}, "branch": {"cc-3"}, "return": {"//evil.example"},
	}
	rec := do(t, server, http.MethodPost, "/ticket", req{Form: form})
	if got := rec.Location; got != "/" {
		t.Errorf("location = %q, want /", got)
	}
}
