package cc_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
	"github.com/O-Marsters-1997/command-center/internal/gh"
	"github.com/O-Marsters-1997/command-center/internal/verdict"
)

// installFakeGhReady puts a script named gh on PATH that logs every invocation to a file and
// answers `pr ready` per readyFails: exit non-zero without touching CC_GH_LOG's caller-visible
// state, so a test can assert the row stayed untouched after a failed un-draft.
func installFakeGhReady(t *testing.T, readyFails bool) (logPath, binDir string) {
	t.Helper()
	binDir = t.TempDir()
	logPath = filepath.Join(t.TempDir(), "gh.log")
	writeFakeGhReadyScript(t, binDir, logPath, readyFails)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return logPath, binDir
}

func writeFakeGhReadyScript(t *testing.T, binDir, logPath string, readyFails bool) {
	t.Helper()
	fail := "0"
	if readyFails {
		fail = "1"
	}
	script := "#!/bin/sh\n" +
		"echo \"$*\" >> \"" + logPath + "\"\n" +
		"if [ \"$1 $2\" = \"pr ready\" ] && [ \"" + fail + "\" = \"1\" ]; then\n" +
		"  echo 'fake gh: pr ready failed' >&2\n" +
		"  exit 1\n" +
		"fi\n" +
		"exit 0\n"
	if err := os.WriteFile(filepath.Join(binDir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

// draftGateFixture is a consumer ticket with a cross-repo gating blocker, pushed and open, ready
// for applyDraftGate to decide over -- the shape every test below starts from.
type draftGateFixture struct {
	store *cc.Store
	cfg   cc.Config
	ws    cc.Workspace
	at    time.Time
	tip   string
}

func newDraftGateFixture(t *testing.T) draftGateFixture {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "repo"), 0o700); err != nil {
		t.Fatal(err)
	}

	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	consumer := cc.Ticket{
		URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1",
		BlockedBy: []string{"sandbox://PLA-40"},
	}
	blocker := cc.Ticket{URL: "sandbox://PLA-40", Repo: "services", Branch: "pla-40"}
	if err := store.UpsertTickets(t.Context(), []cc.Ticket{consumer, blocker}); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	dispositionAsPushed(t, store, consumer.URL, at)
	const tip = "cc-1-tip"
	if err := store.RecordPush(t.Context(), consumer.URL, tip, "main", "main-tip", at); err != nil {
		t.Fatal(err)
	}

	cfg := cc.Config{
		Repos: []cc.Repo{{Name: "repo", Checkout: filepath.Join(root, "repo"), Checks: verdict.Predicate{Success: "CI"}}},
	}
	ws := cc.Workspace{RunsDir: t.TempDir(), SettingsPath: filepath.Join(t.TempDir(), "agent.json")}
	return draftGateFixture{store: store, cfg: cfg, ws: ws, at: at, tip: tip}
}

func ciCheck(conclusion string) map[string]gh.CheckState {
	return map[string]gh.CheckState{"CI": {Status: "COMPLETED", Conclusion: conclusion}}
}

func draftConsumerPR(blockerState gh.PRState, consumerChecks map[string]gh.CheckState, tip string) cc.Observation {
	return cc.Observation{
		PRs: map[string]gh.PR{
			"cc-1":   {Number: 1, State: gh.Open, IsDraft: true, HeadOid: tip, Checks: consumerChecks},
			"pla-40": {State: blockerState},
		},
		BranchTips: map[string]string{"repo//main": "main-tip"},
	}
}

func countReadyCalls(t *testing.T, logPath string) int {
	t.Helper()
	body, err := os.ReadFile(logPath)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(body), "pr ready")
}

// TestDraftGateStaysDraftWhileGatingBlockerIsOpen covers the AC's first half in isolation: an
// unmerged gating blocker keeps the consumer drafted even once its own verdict is green.
func TestDraftGateStaysDraftWhileGatingBlockerIsOpen(t *testing.T) {
	// Not t.Parallel(): installFakeGhReady uses t.Setenv.
	f := newDraftGateFixture(t)
	logPath, _ := installFakeGhReady(t, false)

	obs := draftConsumerPR(gh.Open, ciCheck("SUCCESS"), f.tip)
	observe := func(context.Context) (cc.Observation, error) { return obs, nil }
	loop := cc.NewLoop(f.store, observe, fixedClock(f.at), f.cfg, f.ws, cc.ProcessRunner{})
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if got := countReadyCalls(t, logPath); got != 0 {
		t.Errorf("pr ready calls = %d, want 0: the gating blocker is still open", got)
	}
}

// TestDraftGateStaysDraftWhileVerdictIsNotGreen covers the AC's second half in isolation: a
// merged gating blocker is not enough on its own -- the consumer's own CI must be green too.
func TestDraftGateStaysDraftWhileVerdictIsNotGreen(t *testing.T) {
	// Not t.Parallel(): installFakeGhReady uses t.Setenv.
	f := newDraftGateFixture(t)
	logPath, _ := installFakeGhReady(t, false)

	obs := draftConsumerPR(gh.Merged, ciCheck("FAILURE"), f.tip)
	observe := func(context.Context) (cc.Observation, error) { return obs, nil }
	loop := cc.NewLoop(f.store, observe, fixedClock(f.at), f.cfg, f.ws, cc.ProcessRunner{})
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if got := countReadyCalls(t, logPath); got != 0 {
		t.Errorf("pr ready calls = %d, want 0: the consumer's own CI has not gone green", got)
	}
}

// TestDraftGateUnDraftsOnceAndCallsReadyExactlyOnce is issue #57 AC3: the tick after both halves
// of the gate are satisfied calls `gh pr ready` exactly once, and never again across nine more
// ticks once the observed PR itself reports isDraft=false, as a real gh pr list would from there.
func TestDraftGateUnDraftsOnceAndCallsReadyExactlyOnce(t *testing.T) {
	// Not t.Parallel(): installFakeGhReady uses t.Setenv.
	f := newDraftGateFixture(t)
	logPath, _ := installFakeGhReady(t, false)

	obs := draftConsumerPR(gh.Open, ciCheck("FAILURE"), f.tip)
	observe := func(context.Context) (cc.Observation, error) { return obs, nil }
	loop := cc.NewLoop(f.store, observe, fixedClock(f.at), f.cfg, f.ws, cc.ProcessRunner{})

	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("tick 1: %v", err)
	}
	if got := countReadyCalls(t, logPath); got != 0 {
		t.Fatalf("pr ready calls after tick 1 = %d, want 0", got)
	}

	// Blocker merges, but the consumer's own CI is still red.
	obs = draftConsumerPR(gh.Merged, ciCheck("FAILURE"), f.tip)
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("tick 2: %v", err)
	}
	if got := countReadyCalls(t, logPath); got != 0 {
		t.Fatalf("pr ready calls after tick 2 = %d, want 0", got)
	}

	// Both halves now hold: this is the tick that must call `gh pr ready`.
	obs = draftConsumerPR(gh.Merged, ciCheck("SUCCESS"), f.tip)
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("tick 3: %v", err)
	}
	if got := countReadyCalls(t, logPath); got != 1 {
		t.Fatalf("pr ready calls after tick 3 = %d, want exactly 1", got)
	}

	// GitHub now reports the PR as ready, as a real observe would from here on.
	obs.PRs["cc-1"] = gh.PR{Number: 1, State: gh.Open, IsDraft: false, HeadOid: f.tip, Checks: ciCheck("SUCCESS")}
	for i := range 9 {
		if err := loop.RunOnce(t.Context()); err != nil {
			t.Fatalf("tick %d: %v", i+4, err)
		}
	}
	if got := countReadyCalls(t, logPath); got != 1 {
		t.Errorf("pr ready calls across ten ticks = %d, want exactly 1", got)
	}
}

// TestDraftGateClosedBlockerNeverReadies is issue #57 AC4: a gating blocker whose PR closed
// unmerged must never un-draft the consumer, and must never be confused with `base gone` --
// that derivation is plan.Unlocked's own, gated on a same-repo blocker, never a gating edge.
func TestDraftGateClosedBlockerNeverReadies(t *testing.T) {
	// Not t.Parallel(): installFakeGhReady uses t.Setenv.
	f := newDraftGateFixture(t)
	logPath, _ := installFakeGhReady(t, false)

	obs := draftConsumerPR(gh.Closed, ciCheck("SUCCESS"), f.tip)
	observe := func(context.Context) (cc.Observation, error) { return obs, nil }
	loop := cc.NewLoop(f.store, observe, fixedClock(f.at), f.cfg, f.ws, cc.ProcessRunner{})

	for i := range 5 {
		if err := loop.RunOnce(t.Context()); err != nil {
			t.Fatalf("tick %d: %v", i+1, err)
		}
	}
	if got := countReadyCalls(t, logPath); got != 0 {
		t.Errorf("pr ready calls = %d, want 0: the gating blocker's PR closed unmerged", got)
	}

	server := cc.NewServer(f.store, fixedClock(f.at), f.cfg.Repos, "")
	page := renderPage(t, server)
	state := rowState(t, page, "sandbox://CC-1")
	if state == "base_gone" {
		t.Error("state = base_gone, which is reserved for a stacking parent, never a gating edge")
	}
}

// TestDraftGateReadyFailureIsRetriedNextTickWithoutAVerb is issue #57 AC6: a failed `gh pr
// ready` leaves the row untouched (still draft) and is retried automatically on the very next
// tick, unlike a push failure -- nothing about the retry is gated on a stored fact.
func TestDraftGateReadyFailureIsRetriedNextTickWithoutAVerb(t *testing.T) {
	// Not t.Parallel(): installFakeGhReady uses t.Setenv.
	f := newDraftGateFixture(t)
	logPath, binDir := installFakeGhReady(t, true)

	obs := draftConsumerPR(gh.Merged, ciCheck("SUCCESS"), f.tip)
	observe := func(context.Context) (cc.Observation, error) { return obs, nil }
	loop := cc.NewLoop(f.store, observe, fixedClock(f.at), f.cfg, f.ws, cc.ProcessRunner{})

	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("tick 1: %v", err)
	}
	if got := countReadyCalls(t, logPath); got != 1 {
		t.Fatalf("pr ready calls after tick 1 = %d, want 1 (the failed attempt)", got)
	}

	events, err := f.store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, "draft_ready_failed", "fake gh: pr ready failed") {
		t.Errorf("events = %+v, want a draft_ready_failed event naming the failure", events)
	}

	// Nothing was latched, so a plain second tick (no verb) retries automatically. Fix the fake
	// gh so this attempt succeeds.
	writeFakeGhReadyScript(t, binDir, logPath, false)
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("tick 2: %v", err)
	}
	if got := countReadyCalls(t, logPath); got != 2 {
		t.Errorf("pr ready calls after tick 2 = %d, want 2 (retried automatically)", got)
	}

	events, err = f.store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, "draft_ready", "") {
		t.Errorf("events = %+v, want a draft_ready event for the successful retry", events)
	}
}

// TestDraftPRCountsAsOpenForASameRepoDependent is issue #57 AC5: a draft PR is still an OPEN
// pull request as far as unlock is concerned, so a same-repo dependent still unlocks off its
// branch -- asserted directly, not merely implied by the absence of a failure elsewhere.
func TestDraftPRCountsAsOpenForASameRepoDependent(t *testing.T) {
	t.Parallel()

	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	tickets := []cc.Ticket{
		{URL: "sandbox://PARENT", Repo: "repo", Branch: "parent"},
		{URL: "sandbox://CHILD", Repo: "repo", Branch: "child", BlockedBy: []string{"sandbox://PARENT"}},
	}
	if err := store.UpsertTickets(t.Context(), tickets); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	obs := cc.Observation{
		Worktrees: map[string]string{"parent": "/repos/parent"},
		PRs:       map[string]gh.PR{"parent": {Number: 1, State: gh.Open, IsDraft: true}},
	}
	if err := store.SaveObservation(t.Context(), obs); err != nil {
		t.Fatal(err)
	}

	repos := []cc.Repo{{Name: "repo", Stacking: true}}
	server := cc.NewServer(store, fixedClock(at), repos, "")
	page := renderPage(t, server)

	if base := rowCellAt(t, page, "sandbox://CHILD", 5); base != "parent" {
		t.Errorf("child's base = %q, want %q: a draft PR is still OPEN for unlock purposes", base, "parent")
	}
	if state := rowState(t, page, "sandbox://CHILD"); state != "ready" {
		t.Errorf("child's state = %q, want ready", state)
	}
}

// TestPushOneOpensADraftPRForATicketWithAGatingEdge is issue #57 AC1's first half: a consumer
// ticket with a cross-repo blocker opens its PR as a draft.
func TestPushOneOpensADraftPRForATicketWithAGatingEdge(t *testing.T) {
	// Not t.Parallel(): installFakeGh and repoWithOrigin both use t.Setenv.
	root, repoPath := repoWithOrigin(t)
	ghLog := installFakeGh(t, false)

	worktreePath := cutWorktree(t, repoPath, "cc-1")
	commitFile(t, worktreePath, "agent.txt", "agent was here\n")

	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	consumer := cc.Ticket{
		URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1",
		BlockedBy: []string{"sandbox://PLA-40"},
	}
	blocker := cc.Ticket{URL: "sandbox://PLA-40", Repo: "services", Branch: "pla-40"}
	if err := store.UpsertTickets(t.Context(), []cc.Ticket{consumer, blocker}); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	dispositionAsPushed(t, store, consumer.URL, at)

	obs := cc.Observation{Worktrees: map[string]string{"cc-1": worktreePath}, PRs: map[string]gh.PR{}}
	observe := func(context.Context) (cc.Observation, error) { return obs, nil }

	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	loop := cc.NewLoop(store, observe, fixedClock(at), cfg, ws, cc.ProcessRunner{})
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	body, err := os.ReadFile(ghLog)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "--draft") {
		t.Errorf("gh log %q does not contain --draft for a ticket with a gating edge", body)
	}
}

// TestPushOneOpensANonDraftPRWithNoGatingEdge is issue #57 AC1's parity clause: a plain
// single-repo ticket opens a non-draft PR exactly as in Phase 2.
func TestPushOneOpensANonDraftPRWithNoGatingEdge(t *testing.T) {
	// Not t.Parallel(): installFakeGh and repoWithOrigin both use t.Setenv.
	root, repoPath := repoWithOrigin(t)
	ghLog := installFakeGh(t, false)

	worktreePath := cutWorktree(t, repoPath, "cc-1")
	commitFile(t, worktreePath, "agent.txt", "agent was here\n")

	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	ticket := cc.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTickets(t.Context(), []cc.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	dispositionAsPushed(t, store, ticket.URL, at)

	obs := cc.Observation{Worktrees: map[string]string{"cc-1": worktreePath}, PRs: map[string]gh.PR{}}
	observe := func(context.Context) (cc.Observation, error) { return obs, nil }

	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	loop := cc.NewLoop(store, observe, fixedClock(at), cfg, ws, cc.ProcessRunner{})
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	body, err := os.ReadFile(ghLog)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "--draft") {
		t.Errorf("gh log %q contains --draft for a plain ticket; Phase 2 parity requires none", body)
	}
}
