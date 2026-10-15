package web_test

import (
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/web/webtest"

	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestPageRendersStackDepthAndMergeOrderForAFiveRowStack(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
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

	at := testNow
	prs := map[plan.BranchRef]plan.PR{}
	dispositionAsPushed(t, store, "sandbox://ROOT", at)
	if err := store.RecordPush(ctx, storepkg.Push{
		Ticket: "sandbox://ROOT", Tip: "root-tip", BaseBranch: "main", BaseSHA: "main-tip", At: at,
	}); err != nil {
		t.Fatalf("RecordPush(root): %v", err)
	}
	prs[branchRef("repo", "root")] = plan.PR{Number: 1, State: plan.Open, HeadOid: "root-tip"}

	for _, c := range children {
		branch := strings.ToLower(c)
		ticket := "sandbox://" + c
		dispositionAsPushed(t, store, ticket, at)
		if err := store.RecordPush(ctx, storepkg.Push{
			Ticket: plan.TicketURL(ticket), Tip: plan.SHA(branch + "-tip"), BaseBranch: "root", BaseSHA: "root-tip", At: at,
		}); err != nil {
			t.Fatalf("RecordPush(%q): %v", ticket, err)
		}
		prs[branchRef("repo", branch)] = plan.PR{Number: 2, State: plan.Open, HeadOid: plan.SHA(branch + "-tip")}
	}

	if err := store.SaveObservation(ctx, plan.Observation{
		BranchTips: map[plan.BranchRef]plan.SHA{branchRef("repo", "root"): "root-tip"},
		PRs:        prs,
		Settings:   map[string]config.RepoSettings{"repo": {Stacking: true}},
	}); err != nil {
		t.Fatalf("SaveObservation: %v", err)
	}

	repos := named("repo")
	server := openServer(track(t, store, repos...), fixedClock(at), "")
	page := renderPage(t, server)

	root := webtest.RowFor(t, page, "sandbox://ROOT")
	if got := root.Stack.Text(); got != "L1" {
		t.Errorf("root stack = %q, want L1 (merge order 1, based on main):\n%s", got, root.Row.HTML())
	}
	for _, c := range children {
		ticket := "sandbox://" + c
		row := webtest.RowFor(t, page, ticket)
		if got := row.Stack.Text(); got != "L2 ← root" {
			t.Errorf("%s stack = %q, want L2 ← root (merge order 2, based on root):\n%s", c, got, row.Row.HTML())
		}
	}
}

func TestPageWarnsOnANonMainReadyToMergeLabel(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	tickets := []storepkg.Ticket{
		{URL: "sandbox://PARENT", Repo: "repo", Branch: "parent"},
		{URL: "sandbox://CHILD", Repo: "repo", Branch: "child", BlockedBy: []string{"sandbox://PARENT"}},
	}
	storetest.Tickets(t, store, tickets...)

	at := testNow
	dispositionAsPushed(t, store, "sandbox://PARENT", at)
	dispositionAsPushed(t, store, "sandbox://CHILD", at)
	if err := store.RecordPush(ctx, storepkg.Push{
		Ticket: "sandbox://PARENT", Tip: "parent-tip", BaseBranch: "main", BaseSHA: "main-tip", At: at,
	}); err != nil {
		t.Fatalf("RecordPush(parent): %v", err)
	}
	if err := store.RecordPush(ctx, storepkg.Push{
		Ticket: "sandbox://CHILD", Tip: "child-tip", BaseBranch: "parent", BaseSHA: "parent-tip", At: at,
	}); err != nil {
		t.Fatalf("RecordPush(child): %v", err)
	}

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
	if err := store.SaveObservation(ctx, obs); err != nil {
		t.Fatalf("SaveObservation: %v", err)
	}

	repos := named("repo")
	server := openServer(track(t, store, repos...), fixedClock(at), "")
	page := renderPage(t, server)

	parent := webtest.RowFor(t, page, "sandbox://PARENT")
	if marks := parent.Row.Find("mark.flag-warning"); len(marks) != 0 {
		t.Errorf("root (main-based) has a warning mark, want none:\n%s", parent.Row.HTML())
	}
	child := webtest.RowFor(t, page, "sandbox://CHILD")
	marks := child.Row.Find("mark.flag-warning")
	if len(marks) != 1 || !strings.Contains(marks[0].Text(), "warning") {
		t.Errorf("child (non-main-based) has no warning mark:\n%s", child.Row.HTML())
	} else if title, _ := marks[0].Attr("title"); !strings.Contains(title, "ready-to-merge") {
		t.Errorf("child warning title = %q, want ready-to-merge:\n%s", title, child.Row.HTML())
	}
}

func TestPageWarnsOnARemoveWorktreeRefusal(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	ticket := storepkg.Ticket{URL: "sandbox://MERGED", Repo: "repo", Branch: "merged"}
	storetest.Tickets(t, store, ticket)

	at := testNow
	dispositionAsPushed(t, store, ticket.URL, at)
	if err := store.RecordPush(ctx, storepkg.Push{
		Ticket: plan.TicketURL(ticket.URL), Tip: "merged-tip", BaseBranch: "main", BaseSHA: "main-tip", At: at,
	}); err != nil {
		t.Fatalf("RecordPush(merged): %v", err)
	}
	if err := store.AppendEvent(ctx, storepkg.Event{
		At: at, TicketURL: ticket.URL, Kind: "remove_worktree_refused", Detail: "worktree is dirty",
	}); err != nil {
		t.Fatalf("AppendEvent(remove_worktree_refused): %v", err)
	}

	obs := plan.Observation{
		PRs: map[plan.BranchRef]plan.PR{branchRef("repo", "merged"): {Number: 1, State: plan.Merged, HeadOid: "merged-tip"}},
	}
	if err := store.SaveObservation(ctx, obs); err != nil {
		t.Fatalf("SaveObservation: %v", err)
	}

	server := openServer(track(t, store, named("repo")...), fixedClock(at), "")
	page := renderPage(t, server)

	row := webtest.RowFor(t, page, ticket.URL)
	marks := row.Row.Find("mark.flag-warning")
	if len(marks) != 1 {
		t.Fatalf("state has %d warning marks, want one:\n%s", len(marks), row.Row.HTML())
	}
	got, _ := marks[0].Attr("title")
	if !strings.Contains(got, "worktree removal refused: worktree is dirty") {
		t.Errorf("state warning = %q, want the refusal's detail:\n%s", got, row.Row.HTML())
	}
	if strings.Contains(got, "ready-to-merge") {
		t.Errorf("state warning = %q, want the generic warning label:\n%s", got, row.Row.HTML())
	}
}
