package cc_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
	"github.com/O-Marsters-1997/command-center/internal/gh"
)

// TestPageRendersStackDepthAndMergeOrderForAFiveRowStack covers issue #34's "the page shows
// stack depth and merge order" for a one-root, four-dependent fan-out (issue #33's own shape):
// the root merges first, and every dependent sits one hop below it, whichever order they cut in.
func TestPageRendersStackDepthAndMergeOrderForAFiveRowStack(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))

	tasks := []cc.Task{{TicketURL: "sandbox://ROOT", Repo: "repo", Branch: "root"}}
	children := []string{"CC-2", "CC-3", "CC-4", "CC-5"}
	for _, c := range children {
		tasks = append(tasks, cc.Task{
			TicketURL: "sandbox://" + c, Repo: "repo", Branch: strings.ToLower(c),
			BlockedBy: []string{"sandbox://ROOT"},
		})
	}
	if err := store.UpsertTasks(ctx, tasks); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	prs := map[string]gh.PR{}
	dispositionAsPushed(t, store, "sandbox://ROOT", at)
	if err := store.RecordPush(ctx, "sandbox://ROOT", "root-tip", "main", "main-tip", at); err != nil {
		t.Fatal(err)
	}
	prs["root"] = gh.PR{Number: 1, State: gh.Open, HeadOid: "root-tip"}

	for _, c := range children {
		branch := strings.ToLower(c)
		ticket := "sandbox://" + c
		dispositionAsPushed(t, store, ticket, at)
		if err := store.RecordPush(ctx, ticket, branch+"-tip", "root", "root-tip", at); err != nil {
			t.Fatal(err)
		}
		prs[branch] = gh.PR{Number: 2, State: gh.Open, HeadOid: branch + "-tip"}
	}

	if err := store.SaveObservation(ctx, cc.Observation{
		BranchTips: map[string]string{"root": "root-tip"},
		PRs:        prs,
	}); err != nil {
		t.Fatal(err)
	}

	repos := []cc.Repo{{Name: "repo", Stacking: true}}
<<<<<<< HEAD
	server := cc.NewServer(store, fixedClock(at), repos, "")
=======
	server := cc.NewServer(store, fixedClock(at), repos, nil, "")
>>>>>>> origin/main
	page := renderPage(t, server)

	if got := rowCellAt(t, page, "sandbox://ROOT", 8); got != "0" {
		t.Errorf("root stack depth = %q, want 0", got)
	}
	if got := rowCellAt(t, page, "sandbox://ROOT", 9); got != "1" {
		t.Errorf("root merge order = %q, want 1", got)
	}
	for _, c := range children {
		ticket := "sandbox://" + c
		if got := rowCellAt(t, page, ticket, 8); got != "1" {
			t.Errorf("%s stack depth = %q, want 1", c, got)
		}
		if got := rowCellAt(t, page, ticket, 9); got != "2" {
			t.Errorf("%s merge order = %q, want 2", c, got)
		}
	}
}

// TestPageWarnsOnANonMainReadyToMergeLabel covers issue #34's invariant 2 rendering: a stacked
// row's PR carrying ready-to-merge is flagged, since both repos would squash it into its parent
// branch with the parent's own checks unseen — and the app never applies that label itself.
func TestPageWarnsOnANonMainReadyToMergeLabel(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	tasks := []cc.Task{
		{TicketURL: "sandbox://PARENT", Repo: "repo", Branch: "parent"},
		{TicketURL: "sandbox://CHILD", Repo: "repo", Branch: "child", BlockedBy: []string{"sandbox://PARENT"}},
	}
	if err := store.UpsertTasks(ctx, tasks); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	dispositionAsPushed(t, store, "sandbox://PARENT", at)
	dispositionAsPushed(t, store, "sandbox://CHILD", at)
	if err := store.RecordPush(ctx, "sandbox://PARENT", "parent-tip", "main", "main-tip", at); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordPush(ctx, "sandbox://CHILD", "child-tip", "parent", "parent-tip", at); err != nil {
		t.Fatal(err)
	}

	obs := cc.Observation{
		BranchTips: map[string]string{"parent": "parent-tip"},
		PRs: map[string]gh.PR{
			"parent": {Number: 1, State: gh.Open, HeadOid: "parent-tip", BaseRef: "main", Labels: []string{"ready-to-merge"}},
			"child":  {Number: 2, State: gh.Open, HeadOid: "child-tip", BaseRef: "parent", Labels: []string{"ready-to-merge"}},
		},
	}
	if err := store.SaveObservation(ctx, obs); err != nil {
		t.Fatal(err)
	}

	repos := []cc.Repo{{Name: "repo", Stacking: true}}
<<<<<<< HEAD
	server := cc.NewServer(store, fixedClock(at), repos, "")
=======
	server := cc.NewServer(store, fixedClock(at), repos, nil, "")
>>>>>>> origin/main
	page := renderPage(t, server)

	if got := rowCellAt(t, page, "sandbox://PARENT", 10); got != "" {
		t.Errorf("root (main-based) warning = %q, want empty even with the label", got)
	}
	got := rowCellAt(t, page, "sandbox://CHILD", 10)
	if !strings.Contains(got, "ready-to-merge") {
		t.Errorf("child (non-main-based) warning = %q, want it to name ready-to-merge", got)
	}
}
