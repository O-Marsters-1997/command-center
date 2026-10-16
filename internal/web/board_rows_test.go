package web_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
	"github.com/O-Marsters-1997/command-center/internal/verdict"
	"github.com/O-Marsters-1997/command-center/internal/web/webtest"
)

func pushedRowStore(t *testing.T, ticket storepkg.Ticket, tip string, obs plan.Observation) *storepkg.Store {
	t.Helper()
	store := openStore(t)
	storetest.Tickets(t, store, ticket)
	dispositionAsPushed(t, store, ticket.URL, testNow)
	storetest.Pushed(t, store, storetest.PushedOpts{
		URL: ticket.URL, Tip: tip, Base: "main", BaseTip: "main-tip", At: testNow,
	})
	if err := store.SaveObservation(t.Context(), obs); err != nil {
		t.Fatalf("SaveObservation: %v", err)
	}
	return store
}

func trackedPage(t *testing.T, store *storepkg.Store) string {
	t.Helper()
	return renderPage(t, openServer(track(t, store, named("repo")...), fixedClock(testNow), ""))
}

func testBoardRows(t *testing.T) {
	t.Parallel()

	t.Run("the page nests the board swap's bytes", func(t *testing.T) {
		t.Parallel()
		server := seededServer(t)
		full := renderPath(t, server, "/tickets")
		swap := strings.TrimSpace(renderPath(t, server, "/board?all=1"))

		if !strings.Contains(full, swap) {
			t.Errorf("GET /tickets does not nest GET /board?all=1; board refs = %v, page refs = %v",
				ticketRefs(webtest.BoardRows(t, swap)), ticketRefs(webtest.BoardRows(t, full)))
		}
	})

	t.Run("a stacked row carries its parent's verdict", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		storetest.Tickets(t, store,
			storepkg.Ticket{URL: "sandbox://PARENT", Repo: "repo", Branch: "parent"},
			storepkg.Ticket{URL: "sandbox://CHILD", Repo: "repo", Branch: "child", BlockedBy: []string{"sandbox://PARENT"}},
		)
		dispositionAsPushed(t, store, "sandbox://PARENT", testNow)
		dispositionAsPushed(t, store, "sandbox://CHILD", testNow)
		storetest.Pushed(t, store, storetest.PushedOpts{
			URL: "sandbox://PARENT", Tip: "parent-tip", Base: "main", BaseTip: "main-tip", At: testNow,
		})
		storetest.Pushed(t, store, storetest.PushedOpts{
			URL: "sandbox://CHILD", Tip: "child-tip", Base: "parent", BaseTip: "parent-tip", At: testNow,
		})
		obs := plan.Observation{
			Worktrees: map[plan.BranchRef]string{
				branchRef("repo", "parent"): "/repos/parent", branchRef("repo", "child"): "/repos/child",
			},
			BranchTips: map[plan.BranchRef]plan.SHA{
				branchRef("repo", "parent"): "parent-tip", branchRef("repo", plan.DefaultBaseBranch): "main-tip",
			},
			PRs: map[plan.BranchRef]plan.PR{
				branchRef("repo", "parent"): {
					Number: 1, State: plan.Open, HeadOid: "parent-tip",
					Checks: map[string]plan.CheckState{"CI": {Status: "COMPLETED", Conclusion: "FAILURE"}},
				},
				branchRef("repo", "child"): {
					Number: 2, State: plan.Open, HeadOid: "child-tip",
					Checks: map[string]plan.CheckState{"CI": {Status: "COMPLETED", Conclusion: "SUCCESS"}},
				},
			},
			Settings: map[string]config.RepoSettings{"repo": {Stacking: true, Checks: verdict.Predicate{Success: "CI"}}},
		}
		if err := store.SaveObservation(t.Context(), obs); err != nil {
			t.Fatalf("SaveObservation: %v", err)
		}

		page := trackedPage(t, store)
		if got := webtest.RowFor(t, page, "sandbox://PARENT").State; got != "ci_failed" {
			t.Fatalf("GET /tickets PARENT state = %q, want ci_failed", got)
		}
		webtest.RowFor(t, page, "sandbox://CHILD").Row.One(t, `[title="base verdict: ci_failed"]`)
	})

	t.Run("a CI-failed row links each red required check", func(t *testing.T) {
		t.Parallel()
		ticket := storepkg.Ticket{URL: "sandbox://CI", Repo: "repo", Branch: "ci"}
		store := pushedRowStore(t, ticket, "ci-tip", plan.Observation{
			BranchTips: map[plan.BranchRef]plan.SHA{branchRef("repo", plan.DefaultBaseBranch): "main-tip"},
			PRs: map[plan.BranchRef]plan.PR{branchRef("repo", "ci"): {
				Number: 1, State: plan.Open, HeadOid: "ci-tip",
				Checks: map[string]plan.CheckState{
					"CI":     {Status: "COMPLETED", Conclusion: "FAILURE", DetailsURL: "https://github.com/o/r/actions/runs/1"},
					"Deploy": {Status: "COMPLETED", Conclusion: "FAILURE", DetailsURL: "https://github.com/o/r/actions/runs/2"},
					"Lint":   {Status: "COMPLETED", Conclusion: "FAILURE"},
				},
			}},
			Settings: map[string]config.RepoSettings{"repo": {Checks: verdict.Predicate{AllOf: []verdict.Predicate{
				{Success: "CI"}, {Success: "Deploy"}, {Success: "Lint"},
			}}}},
		})

		row := webtest.RowFor(t, trackedPage(t, store), ticket.URL)
		if row.State != "ci_failed" {
			t.Fatalf("GET /tickets CI state = %q, want ci_failed", row.State)
		}
		var got []map[string]string
		for _, link := range row.Ticket.Find("a[href]") {
			a := attrs(link, "href", "target", "rel")
			a["text"] = link.Text()
			got = append(got, a)
		}
		want := []map[string]string{
			{"href": "https://github.com/o/r/actions/runs/1", "target": "_blank", "rel": "noopener", "text": "CI"},
			{"href": "https://github.com/o/r/actions/runs/2", "target": "_blank", "rel": "noopener", "text": "Deploy"},
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("GET /tickets CI check links mismatch (-want +got):\n%s", diff)
		}
		if !strings.Contains(row.Ticket.Text(), "Lint") {
			t.Errorf("GET /tickets CI ticket cell = %q, want it to name Lint", row.Ticket.Text())
		}
	})

	t.Run("only a red compat check waits on the producer's deploy", func(t *testing.T) {
		t.Parallel()
		ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
		compat := "GraphQL production compatibility"
		store := pushedRowStore(t, ticket, "cc-1-tip", plan.Observation{
			Worktrees:  map[plan.BranchRef]string{branchRef("repo", "cc-1"): "/repos/cc-1"},
			BranchTips: map[plan.BranchRef]plan.SHA{branchRef("repo", plan.DefaultBaseBranch): "main-tip"},
			PRs: map[plan.BranchRef]plan.PR{branchRef("repo", "cc-1"): {
				Number: 1, State: plan.Open, HeadOid: "cc-1-tip",
				Checks: map[string]plan.CheckState{
					compat:  {Status: "COMPLETED", Conclusion: "FAILURE"},
					"Tests": {Status: "COMPLETED", Conclusion: "SUCCESS"},
				},
			}},
			Settings: map[string]config.RepoSettings{"repo": {
				CompatCheck: compat,
				Checks:      verdict.Predicate{AllOf: []verdict.Predicate{{Success: compat}, {Success: "Tests"}}},
			}},
		})

		row := webtest.RowFor(t, trackedPage(t, store), ticket.URL)
		if row.State != "waiting_on_producer_deploy" {
			t.Fatalf("GET /tickets CC-1 state = %q, want waiting_on_producer_deploy", row.State)
		}
		row.Row.One(t, `button[name="verb"][value="re-run"]`)
	})

	t.Run("the verdict uses the checks the observation carries", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name      string
			checks    verdict.Predicate
			wantState string
		}{
			{name: "a required check that passed", checks: verdict.Predicate{Success: "CI"}, wantState: "review_me"},
			{name: "a required check never reported", checks: verdict.Predicate{Success: "Lint"}, wantState: "checking"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				ticket := storepkg.Ticket{URL: "sandbox://CI", Repo: "repo", Branch: "ci"}
				store := pushedRowStore(t, ticket, "ci-tip", plan.Observation{
					BranchTips: map[plan.BranchRef]plan.SHA{branchRef("repo", plan.DefaultBaseBranch): "main-tip"},
					PRs: map[plan.BranchRef]plan.PR{branchRef("repo", "ci"): {
						Number: 1, State: plan.Open, HeadOid: "ci-tip",
						Checks: map[string]plan.CheckState{"CI": {Status: "COMPLETED", Conclusion: "SUCCESS"}},
					}},
					Settings: map[string]config.RepoSettings{"repo": {Checks: tt.checks}},
				})

				if got := webtest.RowFor(t, trackedPage(t, store), ticket.URL).State; got != tt.wantState {
					t.Errorf("GET /tickets CI state = %q, want %q", got, tt.wantState)
				}
			})
		}
	})

	t.Run("a running row shows its elapsed time and kill", func(t *testing.T) {
		t.Parallel()
		ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"}
		now := testNow.Add(90 * time.Second)

		row := webtest.RowFor(t, renderPage(t, newServer(runningRowStore(t, ticket, testNow, now), now)), ticket.URL)
		if row.State != "running" || row.Elapsed.Text() != "1m30s" {
			t.Errorf("GET /tickets CC-1 = %q elapsed %q, want running elapsed 1m30s", row.State, row.Elapsed.Text())
		}
		row.Row.One(t, `button[name="verb"][value="kill"]`)
	})

	t.Run("queued verbs show beside the state until consumed", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"}
		now := testNow.Add(90 * time.Second)
		store := runningRowStore(t, ticket, testNow, now)
		server := newServer(store, now)
		state := func() string {
			t.Helper()
			row := webtest.RowFor(t, renderPage(t, server), ticket.URL)
			row.Row.One(t, `button[name="verb"][value="kill"]`)
			return row.State
		}

		if err := store.QueueVerbIntent(ctx, storepkg.VerbRequest{Ticket: ticket.URL, Verb: "kill", At: now}); err != nil {
			t.Fatalf("QueueVerbIntent(kill): %v", err)
		}
		if got := state(); got != "running · kill queued" {
			t.Errorf("GET /tickets after queuing kill = %q, want running · kill queued", got)
		}
		if err := store.QueueVerbIntent(ctx, storepkg.VerbRequest{
			Ticket: ticket.URL, Verb: "follow-up", At: now.Add(time.Second),
		}); err != nil {
			t.Fatalf("QueueVerbIntent(follow-up): %v", err)
		}
		if got := state(); got != "running · kill queued · follow-up queued" {
			t.Errorf("GET /tickets after queuing follow-up = %q, want both queued", got)
		}
		pending, err := store.PendingVerbIntents(ctx, "kill")
		if err != nil {
			t.Fatalf("PendingVerbIntents(kill): %v", err)
		}
		if err := store.ConsumeVerbIntent(ctx, pending[0].ID, now.Add(2*time.Second)); err != nil {
			t.Fatalf("ConsumeVerbIntent(%d): %v", pending[0].ID, err)
		}
		if got := state(); got != "running · follow-up queued" {
			t.Errorf("GET /tickets after consuming kill = %q, want running · follow-up queued", got)
		}
	})

	t.Run("a queued launch shows before the tick authorises it", func(t *testing.T) {
		t.Parallel()
		store := seededStore(t, testNow)
		if err := store.QueueLaunchIntent(t.Context(), storepkg.LaunchRequest{
			Ticket: "sandbox://CC-1", PromptHash: "hash-1", Group: "group-a", At: testNow,
		}); err != nil {
			t.Fatalf("QueueLaunchIntent: %v", err)
		}
		server := newServer(store, testNow)

		if got := webtest.RowFor(t, renderPage(t, server), "sandbox://CC-1").State; got != "ready · launch queued" {
			t.Errorf("GET /tickets before the tick CC-1 = %q, want ready · launch queued", got)
		}
		if err := store.ApplyLaunchIntents(t.Context(), testNow.Add(15*time.Second)); err != nil {
			t.Fatalf("ApplyLaunchIntents: %v", err)
		}
		if got := webtest.RowFor(t, renderPage(t, server), "sandbox://CC-1").State; got != "queued" {
			t.Errorf("GET /tickets after the tick CC-1 = %q, want queued", got)
		}
	})
}
