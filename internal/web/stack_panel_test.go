package web_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
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
	if err := st.UpsertTickets(ctx, tickets); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveObservation(ctx, plan.Observation{ObservedAt: testNow}); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestSessionStackPanelListsBlockersAndUnlocks(t *testing.T) {
	t.Parallel()
	body := renderPath(t, newServer(stackStore(t), testNow), "/s/acme/web/2")
	panel := body[strings.Index(body, `class="stack-panel"`):]
	blockedBy, unlocks, _ := strings.Cut(panel, "Merging this unlocks")
	if !strings.Contains(blockedBy, `href="/s/acme/web/1"`) {
		t.Errorf("blockers lack #1:\n%s", blockedBy)
	}
	for _, ref := range []string{"/s/acme/web/3", "/s/acme/web/4"} {
		if !strings.Contains(unlocks, `href="`+ref+`"`) {
			t.Errorf("unlocks lack %s:\n%s", ref, unlocks)
		}
	}
	if !strings.Contains(unlocks, `name="blocked_by" value="https://github.com/acme/web/issues/1"`) {
		t.Errorf("form lacks the current blocker:\n%s", unlocks)
	}
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
	req := httptest.NewRequest(http.MethodPost, "/ticket", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/s/acme/web/3" {
		t.Fatalf("status = %d, location = %q", rec.Code, rec.Header().Get("Location"))
	}
	if err := st.EditTicket(t.Context(), "https://github.com/acme/web/issues/3", "cc-3",
		[]string{"https://github.com/acme/web/issues/1"}); err != nil {
		t.Fatal(err)
	}
	body := renderPath(t, server, "/s/acme/web/3")
	if !strings.Contains(body, `href="/s/acme/web/1"`) || strings.Contains(body, `href="/s/acme/web/2"`) {
		t.Errorf("panel does not reflect the edit:\n%s", body)
	}
}

func TestTicketEditIgnoresAnOffSiteReturn(t *testing.T) {
	t.Parallel()
	server := newServer(stackStore(t), testNow)
	form := url.Values{"ticket": {"https://github.com/acme/web/issues/3"}, "branch": {"cc-3"}, "return": {"//evil.example"}}
	req := httptest.NewRequest(http.MethodPost, "/ticket", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if got := rec.Header().Get("Location"); got != "/" {
		t.Errorf("location = %q, want /", got)
	}
}
