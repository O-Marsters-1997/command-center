package web_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
	"github.com/O-Marsters-1997/command-center/internal/web/htmltest"
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
		t.Fatalf("InsertRunSkeleton: %v", err)
	}
	if err := st.RecordSpawn(t.Context(), storepkg.Spawn{
		RunID: plan.RunID(runID), Pgid: 222, StartedAt: testNow, LogPath: "/state/runs/3.jsonl",
	}); err != nil {
		t.Fatalf("RecordSpawn: %v", err)
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
		t.Fatalf("SaveObservation: %v", err)
	}
	return st
}

func testRailLinksSwapMain(t *testing.T) {
	t.Parallel()

	links := htmltest.Parse(t, renderPath(t, newServer(railStore(t, true), testNow), "/rail")).Find("a.rail-link")
	if len(links) == 0 {
		t.Fatal("GET /rail renders no links")
	}
	want := map[string]string{"hx-select": "#main", "hx-target": "#main", "hx-push-url": "true"}
	for _, link := range links {
		if diff := cmp.Diff(want, attrs(link, "hx-select", "hx-target", "hx-push-url")); diff != "" {
			t.Errorf("GET /rail link %s mismatch (-want +got):\n%s", link.HTML(), diff)
		}
	}
}

func testRailSettled(t *testing.T) {
	t.Parallel()

	withRail := htmltest.Parse(t, renderPath(t, newServer(railStore(t, true), testNow), "/rail")).One(t, "#rail-body")
	withRail.One(t, `[data-group="settled"]`).One(t, `button[value="remove-worktree"]`)
	withoutRail := htmltest.Parse(t, renderPath(t, newServer(railStore(t, false), testNow), "/rail")).One(t, "#rail-body")
	if settled := withoutRail.Find(`[data-group="settled"]`); len(settled) != 0 {
		t.Errorf("GET /rail keeps the settled group once the worktree is gone: %s", settled[0].HTML())
	}
}

func testRailRequestState(t *testing.T) {
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

func testRailLoopError(t *testing.T) {
	t.Parallel()

	st := railStore(t, true)
	tickErr := storepkg.TickError{At: testNow.Add(time.Second), Message: "gh exploded"}
	if err := st.RecordTickError(t.Context(), tickErr); err != nil {
		t.Fatalf("RecordTickError: %v", err)
	}
	body := renderPath(t, newServer(st, testNow), "/rail")
	footer := htmltest.Parse(t, body).One(t, "#rail-footer")
	if got := footer.Text(); !strings.Contains(got, "gh exploded") {
		t.Errorf("rail error not shown: %q", got)
	}
}

func testRailQuery(t *testing.T) {
	t.Parallel()

	server := newServer(railStore(t, true), testNow)
	body := renderPath(t, server, "/rail?q=billing")

	rail := htmltest.Parse(t, body).One(t, "#rail-body")
	for _, selector := range []string{`[data-rail-forced]`, `[data-group="settled"]`} {
		if got := rail.Find(selector); len(got) == 0 {
			t.Errorf("GET /rail?q=billing is missing %s", selector)
		}
	}
	plain := htmltest.Parse(t, renderPath(t, server, "/rail")).One(t, "#rail-body")
	if forced := plain.Find("[data-rail-forced]"); len(forced) != 0 {
		t.Errorf("unqueried rail forces open a feature: %s", forced[0].HTML())
	}
}

func testRailQueryMissing(t *testing.T) {
	t.Parallel()

	body := renderPath(t, newServer(railStore(t, true), testNow), "/rail?q=zzzz")
	rail := htmltest.Parse(t, body).One(t, "#rail-body")
	if got := rail.Text(); !strings.Contains(got, "zzzz") {
		t.Errorf("GET /rail?q=zzzz text = %q, want it to say nothing matches zzzz", got)
	}
	if groups := rail.Find("[data-group]"); len(groups) != 0 {
		t.Errorf("unmatched query renders a section: %s", groups[0].HTML())
	}
}

func railHealth(t *testing.T, store *storepkg.Store, now time.Time) []htmltest.Node {
	t.Helper()
	return htmltest.Parse(t, renderPath(t, newServer(store, now), "/rail")).Find(".rail-health")
}

func TestRail(t *testing.T) {
	t.Parallel()

	t.Run("every link swaps main", testRailLinksSwapMain)
	t.Run("drops a settled ticket once its worktree is gone", testRailSettled)
	t.Run("keeps the expanded set and selection from the request", testRailRequestState)
	t.Run("shows a loop error in the footer", testRailLoopError)
	t.Run("a query narrows and forces open without touching the cookie", testRailQuery)
	t.Run("a query with no match anywhere says so", testRailQueryMissing)
	t.Run("an empty needs-you section is absent", func(t *testing.T) {
		t.Parallel()
		now := testNow
		rail := htmltest.Parse(t, renderPath(t, newServer(shellStore(t, &now, ""), now), "/rail"))
		if got := rail.Find(`[data-group="needs-you"]`); len(got) != 0 {
			t.Errorf("GET /rail with nothing needing you renders %s", got[0].HTML())
		}
	})
	t.Run("narrows to the project in the current URL", testRailProjectScope)
	t.Run("renders the latest stored reading", testRailLatestReading)
	t.Run("gauges survive a repeated poll", testRailGaugesStable)
	t.Run("a stale reading shows its age", testRailStaleReading)
	t.Run("insights limits say no reading", testInsightsNoReading)

	t.Run("the observe chip reads stale at twenty seconds", func(t *testing.T) {
		t.Parallel()
		observedAt := testNow
		for _, tt := range []struct {
			name     string
			age      time.Duration
			never    bool
			wantChip string
		}{
			{name: "fresh", age: 2 * time.Second},
			{name: "under twenty", age: 19 * time.Second},
			{name: "at twenty", age: 20 * time.Second, wantChip: "last good observe: 20s ago"},
			{name: "well past", age: 5 * time.Minute, wantChip: "last good observe: 5m0s ago"},
			{name: "no observation", never: true, wantChip: "last good observe: never"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				at := &observedAt
				if tt.never {
					at = nil
				}
				chips := railHealth(t, shellStore(t, at, ""), observedAt.Add(tt.age))
				switch {
				case tt.wantChip == "" && len(chips) != 0:
					t.Errorf("GET /rail chip = %q, want none", chips[0].Text())
				case tt.wantChip != "" && (len(chips) != 1 || !strings.Contains(chips[0].Text(), tt.wantChip)):
					t.Errorf("GET /rail chips = %d, want one reading %q", len(chips), tt.wantChip)
				}
			})
		}
	})

	t.Run("the age carries the absolute instant", func(t *testing.T) {
		t.Parallel()
		body := renderPath(t, newServer(seededStore(t, testNow), testNow.Add(90*time.Second)), "/rail")
		if got := htmltest.Parse(t, body).One(t, `time[datetime="2026-08-20T12:00:15Z"]`).Text(); got != "1m15s ago" {
			t.Errorf("GET /rail clock = %q, want 1m15s ago", got)
		}
	})

	t.Run("the banner only opens on a failed tick", func(t *testing.T) {
		t.Parallel()
		observedAt := testNow
		now := observedAt.Add(45 * time.Second)

		fresh := newServer(shellStore(t, &observedAt, ""), observedAt.Add(2*time.Second))
		quiet := htmltest.Parse(t, renderPath(t, fresh, "/rail"))
		if banners := quiet.Find(".rail-health.banner"); len(banners) != 0 {
			t.Errorf("GET /rail with no failed tick opened a banner: %s", banners[0].HTML())
		}
		failed := newServer(shellStore(t, &observedAt, "gh is unavailable"), now)
		banner := htmltest.Parse(t, renderPath(t, failed, "/rail")).One(t, ".rail-health.banner")
		for _, want := range []string{"tick failed 44s ago", "gh is unavailable"} {
			if got := banner.Text(); !strings.Contains(got, want) {
				t.Errorf("GET /rail banner = %q, want %q", got, want)
			}
		}
	})

	t.Run("the banner closes once a tick succeeds", func(t *testing.T) {
		t.Parallel()
		observedAt := testNow
		store := shellStore(t, &observedAt, "")
		tickErr := storepkg.TickError{At: observedAt.Add(-30 * time.Second), Message: "gh is unavailable"}
		if err := store.RecordTickError(t.Context(), tickErr); err != nil {
			t.Fatalf("RecordTickError: %v", err)
		}

		rail := htmltest.Parse(t, renderPath(t, newServer(store, observedAt.Add(2*time.Second)), "/rail"))
		if banners := rail.Find(".rail-health"); len(banners) != 0 {
			t.Errorf("GET /rail keeps the banner after recovery: %s", banners[0].HTML())
		}
	})

	t.Run("the scope menu counts running tickets and omits zero", func(t *testing.T) {
		t.Parallel()
		now := testNow
		for _, tt := range []struct {
			name  string
			store func(*testing.T) *storepkg.Store
			want  int
		}{
			{
				name: "one running",
				store: func(t *testing.T) *storepkg.Store {
					t.Helper()
					return detailStore(t, writeLog(t, 1), now, now)
				},
				want: 1,
			},
			{
				name: "idle",
				store: func(t *testing.T) *storepkg.Store {
					t.Helper()
					return shellStore(t, &now, "")
				},
			},
			{name: "a run whose row reads base gone", store: baseGoneLiveStore, want: 1},
		} {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				page := htmltest.Parse(t, renderPath(t, newServer(tt.store(t), now), "/tickets"))
				counts := page.Find("[data-scope-menu] summary .rail-count")
				switch {
				case tt.want == 0 && len(counts) != 0:
					t.Errorf("GET /tickets scope = %q, want no running count", counts[0].Text())
				case tt.want > 0 && (len(counts) != 1 || counts[0].Text() != fmt.Sprintf("%d running", tt.want)):
					t.Errorf("GET /tickets scope counts = %d, want one reading %d running", len(counts), tt.want)
				}
			})
		}
	})

	t.Run("verb buttons carry no key", func(t *testing.T) {
		t.Parallel()
		failed, _ := sessionStoreEnding(t, sessionRunOpts{
			Kind: "agent", Prompt: "p", KeepPrompt: true, Outcome: plan.OutcomeFailed,
		})
		rail := htmltest.Parse(t, renderPath(t, newServer(failed, testNow), "/rail"))
		buttons := rail.One(t, "#rail-body").Find(`button[name="verb"]`)
		if len(buttons) == 0 {
			t.Fatal("GET /rail has no verb buttons")
		}
		for _, button := range buttons {
			if key, ok := button.Attr("data-key"); ok {
				t.Errorf("GET /rail verb carries key %q: %s", key, button.HTML())
			}
		}
	})
}

func baseGoneLiveStore(t *testing.T) *storepkg.Store {
	t.Helper()
	ctx := t.Context()
	store := openStore(t)
	blocker := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	dependent := storepkg.Ticket{URL: "sandbox://CC-2", Repo: "repo", Branch: "cc-2", BlockedBy: []string{blocker.URL}}
	storetest.Tickets(t, store, blocker, dependent)
	runID, err := store.InsertRunSkeleton(ctx, storepkg.RunSkeleton{
		Ticket: dependent.URL, Kind: "agent", BaselineSHA: "basesha1234", PromptHash: "hash-1",
	})
	if err != nil {
		t.Fatalf("InsertRunSkeleton: %v", err)
	}
	if err := store.RecordSpawn(ctx, storepkg.Spawn{
		RunID: plan.RunID(runID), Pgid: 4242, StartedAt: testNow, LogPath: "/logs/run.jsonl",
	}); err != nil {
		t.Fatalf("RecordSpawn: %v", err)
	}
	obs := plan.Observation{
		ObservedAt: testNow,
		Runs:       map[plan.TicketURL]plan.RunObservation{plan.TicketURL(dependent.URL): {Alive: true}},
		PRs:        map[plan.BranchRef]plan.PR{{Branch: "cc-1"}: {State: plan.Closed}},
	}
	if err := store.SaveObservation(ctx, obs); err != nil {
		t.Fatalf("SaveObservation: %v", err)
	}
	return store
}
