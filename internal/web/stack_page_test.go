package web_test

import (
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
	"github.com/O-Marsters-1997/command-center/internal/web/webtest"
)

func recordPush(t *testing.T, store *storepkg.Store, push storepkg.Push) {
	t.Helper()
	if err := store.RecordPush(t.Context(), push); err != nil {
		t.Fatalf("RecordPush(%s): %v", push.Ticket, err)
	}
}

func testBoardStacks(t *testing.T) {
	t.Parallel()

	t.Run("renders stack depth and merge order for a five-row stack", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		tickets := []storepkg.Ticket{{URL: "sandbox://ROOT", Repo: "repo", Branch: "root"}}
		children := []string{"CC-2", "CC-3", "CC-4", "CC-5"}
		for _, c := range children {
			tickets = append(tickets, storepkg.Ticket{
				URL: "sandbox://" + c, Repo: "repo", Branch: strings.ToLower(c),
				BlockedBy: []string{"sandbox://ROOT"},
			})
		}
		storetest.Tickets(t, store, tickets...)

		dispositionAsPushed(t, store, "sandbox://ROOT", testNow)
		recordPush(t, store, storepkg.Push{
			Ticket: "sandbox://ROOT", Tip: "root-tip", BaseBranch: "main", BaseSHA: "main-tip", At: testNow,
		})
		prs := map[plan.BranchRef]plan.PR{branchRef("repo", "root"): {Number: 1, State: plan.Open, HeadOid: "root-tip"}}
		for _, c := range children {
			branch, ticket := strings.ToLower(c), "sandbox://"+c
			dispositionAsPushed(t, store, ticket, testNow)
			recordPush(t, store, storepkg.Push{
				Ticket: plan.TicketURL(ticket), Tip: plan.SHA(branch + "-tip"), BaseBranch: "root", BaseSHA: "root-tip",
				At: testNow,
			})
			prs[branchRef("repo", branch)] = plan.PR{Number: 2, State: plan.Open, HeadOid: plan.SHA(branch + "-tip")}
		}
		if err := store.SaveObservation(t.Context(), plan.Observation{
			BranchTips: map[plan.BranchRef]plan.SHA{branchRef("repo", "root"): "root-tip"},
			PRs:        prs,
			Settings:   map[string]config.RepoSettings{"repo": {Stacking: true}},
		}); err != nil {
			t.Fatalf("SaveObservation: %v", err)
		}

		page := renderPage(t, openServer(track(t, store, named("repo")...), fixedClock(testNow), ""))
		if got := webtest.RowFor(t, page, "sandbox://ROOT").Stack.Text(); got != "L1" {
			t.Errorf("GET /tickets ROOT stack = %q, want L1", got)
		}
		for _, c := range children {
			if got := webtest.RowFor(t, page, "sandbox://"+c).Stack.Text(); got != "L2 ← root" {
				t.Errorf("GET /tickets %s stack = %q, want L2 ← root", c, got)
			}
		}
	})

	t.Run("warns on a ready-to-merge label off main", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		storetest.Tickets(t, store,
			storepkg.Ticket{URL: "sandbox://PARENT", Repo: "repo", Branch: "parent"},
			storepkg.Ticket{URL: "sandbox://CHILD", Repo: "repo", Branch: "child", BlockedBy: []string{"sandbox://PARENT"}},
		)
		dispositionAsPushed(t, store, "sandbox://PARENT", testNow)
		dispositionAsPushed(t, store, "sandbox://CHILD", testNow)
		recordPush(t, store, storepkg.Push{
			Ticket: "sandbox://PARENT", Tip: "parent-tip", BaseBranch: "main", BaseSHA: "main-tip", At: testNow,
		})
		recordPush(t, store, storepkg.Push{
			Ticket: "sandbox://CHILD", Tip: "child-tip", BaseBranch: "parent", BaseSHA: "parent-tip", At: testNow,
		})
		obs := plan.Observation{
			BranchTips: map[plan.BranchRef]plan.SHA{branchRef("repo", "parent"): "parent-tip"},
			PRs: map[plan.BranchRef]plan.PR{
				branchRef("repo", "parent"): {
					Number: 1, State: plan.Open, HeadOid: "parent-tip", BaseRef: "main", Labels: []string{"ready-to-merge"},
				},
				branchRef("repo", "child"): {
					Number: 2, State: plan.Open, HeadOid: "child-tip", BaseRef: "parent", Labels: []string{"ready-to-merge"},
				},
			},
			Settings: map[string]config.RepoSettings{"repo": {Stacking: true}},
		}
		if err := store.SaveObservation(t.Context(), obs); err != nil {
			t.Fatalf("SaveObservation: %v", err)
		}

		page := renderPage(t, openServer(track(t, store, named("repo")...), fixedClock(testNow), ""))
		if marks := webtest.RowFor(t, page, "sandbox://PARENT").Row.Find("mark.flag-warning"); len(marks) != 0 {
			t.Errorf("GET /tickets PARENT has %d warnings, want none on a main-based PR", len(marks))
		}
		marks := webtest.RowFor(t, page, "sandbox://CHILD").Row.Find("mark.flag-warning")
		if len(marks) != 1 {
			t.Fatalf("GET /tickets CHILD has %d warnings, want 1", len(marks))
		}
		if title, _ := marks[0].Attr("title"); !strings.Contains(title, "ready-to-merge") {
			t.Errorf("GET /tickets CHILD warning title = %q, want it to name ready-to-merge", title)
		}
	})

	t.Run("warns on a remove-worktree refusal", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		ticket := storepkg.Ticket{URL: "sandbox://MERGED", Repo: "repo", Branch: "merged"}
		storetest.Tickets(t, store, ticket)
		dispositionAsPushed(t, store, ticket.URL, testNow)
		recordPush(t, store, storepkg.Push{
			Ticket: plan.TicketURL(ticket.URL), Tip: "merged-tip", BaseBranch: "main", BaseSHA: "main-tip", At: testNow,
		})
		if err := store.AppendEvent(t.Context(), storepkg.Event{
			At: testNow, TicketURL: ticket.URL, Kind: "remove_worktree_refused", Detail: "worktree is dirty",
		}); err != nil {
			t.Fatalf("AppendEvent(remove_worktree_refused): %v", err)
		}
		obs := plan.Observation{
			PRs: map[plan.BranchRef]plan.PR{branchRef("repo", "merged"): {Number: 1, State: plan.Merged, HeadOid: "merged-tip"}},
		}
		if err := store.SaveObservation(t.Context(), obs); err != nil {
			t.Fatalf("SaveObservation: %v", err)
		}

		page := renderPage(t, openServer(track(t, store, named("repo")...), fixedClock(testNow), ""))
		marks := webtest.RowFor(t, page, ticket.URL).Row.Find("mark.flag-warning")
		if len(marks) != 1 {
			t.Fatalf("GET /tickets MERGED has %d warnings, want 1", len(marks))
		}
		if got, _ := marks[0].Attr("title"); !strings.Contains(got, "worktree is dirty") ||
			strings.Contains(got, "ready-to-merge") {
			t.Errorf("GET /tickets MERGED warning title = %q, want the refusal's detail", got)
		}
	})
}
