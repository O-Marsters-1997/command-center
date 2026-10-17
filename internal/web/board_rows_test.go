package web_test

import (
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/verdict"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
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
		if row.State != "Waiting on deploy" {
			t.Fatalf("GET /tickets CC-1 state = %q, want Waiting on deploy", row.State)
		}
	})

	t.Run("the verdict uses the checks the observation carries", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name      string
			checks    verdict.Predicate
			wantState string
		}{
			{name: "a required check that passed", checks: verdict.Predicate{Success: "CI"}, wantState: "Ready for review"},
			{
				name:      "a required check never reported",
				checks:    verdict.Predicate{Success: "Lint"},
				wantState: "Waiting on checks",
			},
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

	t.Run("a running row shows its elapsed time", func(t *testing.T) {
		t.Parallel()
		ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"}
		now := testNow.Add(90 * time.Second)

		row := webtest.RowFor(t, renderPage(t, newServer(runningRowStore(t, ticket, testNow, now), now)), ticket.URL)
		if row.State != "Agent running" || !strings.Contains(row.Row.Text(), "1m30s") {
			t.Errorf("GET /tickets CC-1 = %q text %q, want Agent running with 1m30s", row.State, row.Row.Text())
		}
	})
}
