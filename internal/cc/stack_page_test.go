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

	tickets := []cc.Ticket{{URL: "sandbox://ROOT", Repo: "repo", Branch: "root"}}
	children := []string{"CC-2", "CC-3", "CC-4", "CC-5"}
	for _, c := range children {
		tickets = append(tickets, cc.Ticket{
			URL: "sandbox://" + c, Repo: "repo", Branch: strings.ToLower(c),
			BlockedBy: []string{"sandbox://ROOT"},
		})
	}
	if err := store.UpsertTickets(ctx, tickets); err != nil {
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
	server := cc.NewServer(store, fixedClock(at), repos, "")
	page := renderPage(t, server)

	if got := rowCellAt(t, page, "sandbox://ROOT", 4); got != "L1" {
		t.Errorf("root stack = %q, want L1 (merge order 1, based on main)", got)
	}
	for _, c := range children {
		ticket := "sandbox://" + c
		if got := rowCellAt(t, page, ticket, 4); got != "L2 ← root" {
			t.Errorf("%s stack = %q, want L2 ← root (merge order 2, based on root)", c, got)
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
	tickets := []cc.Ticket{
		{URL: "sandbox://PARENT", Repo: "repo", Branch: "parent"},
		{URL: "sandbox://CHILD", Repo: "repo", Branch: "child", BlockedBy: []string{"sandbox://PARENT"}},
	}
	if err := store.UpsertTickets(ctx, tickets); err != nil {
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
	server := cc.NewServer(store, fixedClock(at), repos, "")
	page := renderPage(t, server)

	if got := rowCellAt(t, page, "sandbox://PARENT", 1); strings.Contains(got, "flag-warning") {
		t.Errorf("root (main-based) state cell = %q, want no warning mark even with the label", got)
	}
	got := rowCellAt(t, page, "sandbox://CHILD", 1)
	if !strings.Contains(got, "ready-to-merge") {
		t.Errorf("child (non-main-based) state cell = %q, want a warning mark naming ready-to-merge", got)
	}
}
