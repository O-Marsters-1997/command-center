package web_test

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

func railStore(t *testing.T, withWorktree bool) *storepkg.Store {
	t.Helper()
	st := track(t, openStore(t), named("repo")...)
	tickets := []storepkg.Ticket{
		{URL: "sandbox://CC-1", Repo: "repo", Branch: "one", Feature: "project:billing"},
		{
			URL: "sandbox://CC-2", Repo: "repo", Branch: "two", Feature: "project:billing",
			BlockedBy: []string{"sandbox://CC-1"},
		},
		{URL: "sandbox://CC-3", Repo: "repo", Branch: "three", Feature: "project:billing"},
	}
	if err := st.UpsertTickets(t.Context(), tickets); err != nil {
		t.Fatal(err)
	}
	dispositionAsPushed(t, st, "sandbox://CC-1", testNow)
	runID, err := st.InsertRunSkeleton(t.Context(), "sandbox://CC-3", "agent", "", "hash-3")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.RecordSpawn(t.Context(), runID, 222, testNow, "/state/runs/3.jsonl"); err != nil {
		t.Fatal(err)
	}
	obs := plan.Observation{
		ObservedAt: testNow,
		Runs:       map[string]plan.RunObservation{"sandbox://CC-3": {Alive: true}},
		PRs:        map[string]plan.PR{plan.BranchKey("repo", "one"): {Number: 1, State: plan.Merged}},
	}
	if withWorktree {
		obs.Worktrees = map[string]string{plan.BranchKey("repo", "one"): "/wt/one"}
	}
	if err := st.SaveObservation(t.Context(), obs); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestEveryRailLinkSwapsMain(t *testing.T) {
	t.Parallel()

	rail := renderPath(t, newServer(railStore(t, true), testNow), "/rail")

	links := regexp.MustCompile(`<a class="rail-link"[^>]*>`).FindAllString(rail, -1)
	if len(links) == 0 {
		t.Fatalf("the rail renders no links, so the check below proves nothing:\n%s", rail)
	}
	for _, link := range links {
		for _, attr := range []string{`hx-select="#main"`, `hx-target="#main"`, `hx-push-url="true"`} {
			if !strings.Contains(link, attr) {
				t.Errorf("rail link %s is missing %s", link, attr)
			}
		}
	}
}

func TestRailDropsASettledTicketOnceItsWorktreeIsGone(t *testing.T) {
	t.Parallel()

	with := renderPath(t, newServer(railStore(t, true), testNow), "/rail")
	without := renderPath(t, newServer(railStore(t, false), testNow), "/rail")

	if !strings.Contains(with, `data-group="settled"`) {
		t.Errorf("a merged ticket with a worktree is not under Settled:\n%s", with)
	}
	if strings.Contains(without, `data-group="settled"`) {
		t.Errorf("a merged ticket with no worktree is still under Settled:\n%s", without)
	}
}

func TestRailKeepsTheExpandedSetAndSelectionFromTheRequest(t *testing.T) {
	t.Parallel()

	st, _ := sessionStore(t, "agent", "", false, true)
	server := newServer(st, testNow)
	req := httptest.NewRequest(http.MethodGet, "/rail", nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Current-URL", "http://cc/s/acme/web/1")
	req.AddCookie(&http.Cookie{Name: "rail-open", Value: "feature:checkout"})
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `data-rail-key="feature:checkout" open`) {
		t.Errorf("checkout was open in the cookie but rendered closed:\n%s", body)
	}
	if !strings.Contains(body, `aria-current="page"`) {
		t.Errorf("the ticket in HX-Current-URL is not marked current:\n%s", body)
	}
}

func TestRailShowsALoopErrorInTheFooter(t *testing.T) {
	t.Parallel()

	st := railStore(t, true)
	tickErr := storepkg.TickError{At: testNow.Add(time.Second), Message: "gh exploded"}
	if err := st.RecordTickError(t.Context(), tickErr); err != nil {
		t.Fatal(err)
	}
	body := renderPath(t, newServer(st, testNow), "/rail")
	if !strings.Contains(body, "gh exploded") {
		t.Errorf("the last loop error is missing from the rail footer:\n%s", body)
	}
}

func TestRailQueryNarrowsAndForcesOpenWithoutTouchingTheCookie(t *testing.T) {
	t.Parallel()

	server := newServer(railStore(t, true), testNow)
	body := renderPath(t, server, "/rail?q=billing")

	for _, want := range []string{`data-rail-forced`, `data-group="settled"`, `data-group="needs-you"`} {
		if !strings.Contains(body, want) {
			t.Errorf("GET /rail?q=billing is missing %q:\n%s", want, body)
		}
	}
	if !strings.Contains(body, "No match.") {
		t.Errorf("a section with no match does not say so:\n%s", body)
	}
	if strings.Contains(renderPath(t, server, "/rail"), "data-rail-forced") {
		t.Error("a rail without a query marks features forced")
	}
}

func TestRailQueryWithNoMatchAnywhereSaysSo(t *testing.T) {
	t.Parallel()

	body := renderPath(t, newServer(railStore(t, true), testNow), "/rail?q=zzzz")
	if !strings.Contains(body, "Nothing matches “zzzz”.") {
		t.Errorf("an unmatched query has no summary line:\n%s", body)
	}
	if strings.Contains(body, `data-group=`) {
		t.Errorf("an unmatched query still renders sections:\n%s", body)
	}
}
