package web_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/htmltest"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
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
	storetest.Tickets(t, st, tickets...)
	dispositionAsPushed(t, st, "sandbox://CC-1", testNow)
	runID, err := st.InsertRunSkeleton(t.Context(), storepkg.RunSkeleton{
		Ticket: "sandbox://CC-3", Kind: "agent", PromptHash: "hash-3",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.RecordSpawn(t.Context(), storepkg.Spawn{
		RunID: plan.RunID(runID), Pgid: 222, StartedAt: testNow, LogPath: "/state/runs/3.jsonl",
	}); err != nil {
		t.Fatal(err)
	}
	obs := plan.Observation{
		ObservedAt: testNow,
		Runs:       map[plan.TicketURL]plan.RunObservation{"sandbox://CC-3": {Alive: true}},
		PRs:        map[plan.BranchRef]plan.PR{branchRef("repo", "one"): {Number: 1, State: plan.Merged}},
	}
	if withWorktree {
		obs.Worktrees = map[plan.BranchRef]string{branchRef("repo", "one"): "/wt/one"}
	}
	if err := st.SaveObservation(t.Context(), obs); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestEveryRailLinkSwapsMain(t *testing.T) {
	t.Parallel()

	rail := renderPath(t, newServer(railStore(t, true), testNow), "/rail")

	links := htmltest.Parse(t, rail).Find("a.rail-link")
	if len(links) == 0 {
		t.Fatal("the rail renders no links")
	}
	for _, link := range links {
		for name, want := range map[string]string{"hx-select": "#main", "hx-target": "#main", "hx-push-url": "true"} {
			if got, _ := link.Attr(name); got != want {
				t.Errorf("rail link %s = %q, want %q: %s", name, got, want, link.HTML())
			}
		}
	}
}

func TestRailDropsASettledTicketOnceItsWorktreeIsGone(t *testing.T) {
	t.Parallel()

	with := renderPath(t, newServer(railStore(t, true), testNow), "/rail")
	without := renderPath(t, newServer(railStore(t, false), testNow), "/rail")

	withRail := htmltest.Parse(t, with).One(t, "#rail-body")
	withRail.One(t, `[data-group="settled"]`)
	withoutRail := htmltest.Parse(t, without).One(t, "#rail-body")
	if settled := withoutRail.Find(`[data-group="settled"]`); len(settled) != 0 {
		t.Errorf("settled group persists without worktree: %s", settled[0].HTML())
	}
}

func TestRailKeepsTheExpandedSetAndSelectionFromTheRequest(t *testing.T) {
	t.Parallel()

	st, _ := sessionStore(t, sessionRunOpts{Kind: "agent", Alive: true})
	server := newServer(st, testNow)
	got := do(t, server, http.MethodGet, "/rail", req{Header: http.Header{
		"Hx-Request":     {"true"},
		"Hx-Current-Url": {"http://cc/s/acme/web/1"},
		"Cookie":         {"rail-open=feature:checkout"},
	}})

	rail := htmltest.Parse(t, got.Body).One(t, "#rail-body")
	checkout := rail.One(t, `[data-rail-key="feature:checkout"]`)
	if _, ok := checkout.Attr("open"); !ok {
		t.Errorf("checkout rendered closed: %s", checkout.HTML())
	}
	rail.One(t, `a[aria-current="page"]`)
}

func TestRailShowsALoopErrorInTheFooter(t *testing.T) {
	t.Parallel()

	st := railStore(t, true)
	tickErr := storepkg.TickError{At: testNow.Add(time.Second), Message: "gh exploded"}
	if err := st.RecordTickError(t.Context(), tickErr); err != nil {
		t.Fatal(err)
	}
	body := renderPath(t, newServer(st, testNow), "/rail")
	rail := htmltest.Parse(t, body).One(t, "#rail-body")
	if got := rail.Text(); !strings.Contains(got, "gh exploded") {
		t.Errorf("rail error not shown: %q", got)
	}
}

func TestRailQueryNarrowsAndForcesOpenWithoutTouchingTheCookie(t *testing.T) {
	t.Parallel()

	server := newServer(railStore(t, true), testNow)
	body := renderPath(t, server, "/rail?q=billing")

	rail := htmltest.Parse(t, body).One(t, "#rail-body")
	for _, selector := range []string{`[data-rail-forced]`, `[data-group="settled"]`, `[data-group="needs-you"]`} {
		if got := rail.Find(selector); len(got) == 0 {
			t.Errorf("GET /rail?q=billing is missing %s", selector)
		}
	}
	if got := rail.Text(); !strings.Contains(got, "No match.") {
		t.Errorf("rail empty section = %q", got)
	}
	plain := htmltest.Parse(t, renderPath(t, server, "/rail")).One(t, "#rail-body")
	if forced := plain.Find("[data-rail-forced]"); len(forced) != 0 {
		t.Errorf("unqueried rail forces open a feature: %s", forced[0].HTML())
	}
}

func TestRailQueryWithNoMatchAnywhereSaysSo(t *testing.T) {
	t.Parallel()

	body := renderPath(t, newServer(railStore(t, true), testNow), "/rail?q=zzzz")
	rail := htmltest.Parse(t, body).One(t, "#rail-body")
	if got := rail.Text(); !strings.Contains(got, "Nothing matches “zzzz”.") {
		t.Errorf("unmatched rail summary = %q", got)
	}
	if groups := rail.Find("[data-group]"); len(groups) != 0 {
		t.Errorf("unmatched query renders a section: %s", groups[0].HTML())
	}
}
