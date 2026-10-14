package loop_test

import (
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/gittest"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/option"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

func dispositionAsPushed(t *testing.T, store *storepkg.Store, ticketURL string, at time.Time) {
	t.Helper()
	runID, err := store.InsertRunSkeleton(t.Context(), storepkg.RunSkeleton{
		Ticket: ticketURL, Kind: "agent", PromptHash: "hash-1",
	})
	if err != nil {
		t.Fatalf("InsertRunSkeleton: %v", err)
	}
	spawn := storepkg.Spawn{RunID: plan.RunID(runID), Pgid: 111, StartedAt: at, LogPath: "/state/runs/1.jsonl"}
	if err := store.RecordSpawn(t.Context(), spawn); err != nil {
		t.Fatalf("RecordSpawn: %v", err)
	}
	exitCode := 0
	if err := store.RecordDisposition(t.Context(), storepkg.Disposition{
		RunID: plan.RunID(runID), Outcome: plan.OutcomePush, ExitCode: option.Some(exitCode),
		Cost: option.None[float64](), EndedAt: at,
	}); err != nil {
		t.Fatalf("RecordDisposition: %v", err)
	}
}

func hasEvent(events []storepkg.Event, kind storepkg.EventKind, detailSubstring string) bool {
	for _, e := range events {
		if e.Kind == kind && strings.Contains(e.Detail, detailSubstring) {
			return true
		}
	}
	return false
}

func TestPushPushableRefusesAPolicyHitAndNeverPushes(t *testing.T) {
	t.Parallel()
	f := newPushFixture(t)
	gittest.Commit(t, f.Worktree, ".github/workflows/x.yml", "name: x\n")
	dispositionAsPushed(t, f.Store, f.Ticket.URL, testAt)
	f.tick(t)

	if gittest.HasRef(t, f.Origin, "refs/heads/cc-1") {
		t.Error("cc-1 must not exist on the remote: the diff hit the push policy")
	}

	facts, err := f.Store.PushFacts(t.Context())
	if err != nil {
		t.Fatalf("PushFacts: %v", err)
	}
	fact := facts[plan.TicketURL(f.Ticket.URL)]
	if !fact.Refused || fact.RefusedPath != ".github/workflows/x.yml" {
		t.Errorf("push facts = %+v, want refused naming .github/workflows/x.yml", fact)
	}
	events := f.events(t, "push_refused")
	if len(events) != 1 || events[0].TicketURL != f.Ticket.URL ||
		!strings.Contains(events[0].Detail, ".github/workflows/x.yml") {
		t.Errorf("events = %+v, want a push_refused event naming the path", events)
	}
	f.tick(t)
	if got := len(f.events(t, "push_refused")); got != 1 {
		t.Errorf("push_refused events after a second tick = %d, want 1: no auto-retry", got)
	}
}

func TestPushPushablePushesAndCreatesAPROnceThenStaysIdempotent(t *testing.T) {
	t.Parallel()
	f := newPushFixture(t)
	gittest.Commit(t, f.Worktree, "agent.txt", "agent was here\n")
	dispositionAsPushed(t, f.Store, f.Ticket.URL, testAt)
	f.tick(t)

	if !gittest.HasRef(t, f.Origin, "refs/heads/cc-1") {
		t.Fatal("cc-1 was not pushed to the remote")
	}
	calls := f.Forge.Calls("Create")
	if len(calls) != 1 || calls[0].Repo != f.Worktree || calls[0].Base != "main" || calls[0].Draft {
		t.Errorf("Create calls = %+v, want one non-draft PR based on main", calls)
	}

	tips, err := f.Store.LastPushedTips(t.Context())
	if err != nil {
		t.Fatalf("LastPushedTips: %v", err)
	}
	if tips[plan.TicketURL(f.Ticket.URL)] == "" {
		t.Error("no pushes row recorded")
	}

	f.tick(t)
	if got := len(f.Forge.Calls("Create")); got != 1 {
		t.Errorf("Create calls after a second tick = %d, want 1", got)
	}
}

func TestPushPushableAdoptsAnExistingOpenPRRatherThanDuplicating(t *testing.T) {
	t.Parallel()
	f := newPushFixture(t)
	gittest.Commit(t, f.Worktree, "agent.txt", "agent was here\n")
	dispositionAsPushed(t, f.Store, f.Ticket.URL, testAt)
	f.Obs.PRs[branchRef("repo", "cc-1")] = plan.PR{Number: 7, HeadRef: "cc-1", State: plan.Open}
	f.tick(t)

	if !gittest.HasRef(t, f.Origin, "refs/heads/cc-1") {
		t.Error("cc-1 was not pushed to the remote")
	}
	if got := len(f.Forge.Calls("Create")); got != 0 {
		t.Errorf("Create calls = %d, want 0: an open PR must be adopted, never duplicated", got)
	}

	tips, err := f.Store.LastPushedTips(t.Context())
	if err != nil {
		t.Fatalf("LastPushedTips: %v", err)
	}
	if tips[plan.TicketURL(f.Ticket.URL)] == "" {
		t.Error("no pushes row recorded despite adopting the existing PR")
	}
}

func TestPushFailureIsNotRetriedAutomaticallyButRetryPushBypassesTheGate(t *testing.T) {
	t.Parallel()
	f := newPushFixture(t)
	gittest.Commit(t, f.Worktree, "agent.txt", "agent was here\n")
	dispositionAsPushed(t, f.Store, f.Ticket.URL, testAt)
	f.reset(testAt, runner.ProcessRunner{}, loop.WithForge(failingCreate{f.Forge}))
	f.tick(t)
	if !gittest.HasRef(t, f.Origin, "refs/heads/cc-1") {
		t.Fatal("the push itself must still succeed even though pr create fails")
	}
	if got := len(f.Forge.Calls("Create")); got != 1 {
		t.Fatalf("Create calls = %d, want 1 (the failed attempt)", got)
	}

	facts, err := f.Store.PushFacts(t.Context())
	if err != nil {
		t.Fatalf("PushFacts: %v", err)
	}
	if !facts[plan.TicketURL(f.Ticket.URL)].Failed {
		t.Fatal("push facts must record the failure")
	}
	tips, err := f.Store.LastPushedTips(t.Context())
	if err != nil {
		t.Fatalf("LastPushedTips: %v", err)
	}
	if tips[plan.TicketURL(f.Ticket.URL)] != "" {
		t.Error("no pushes row until the PR is actually opened")
	}

	f.tick(t)
	if got := len(f.Forge.Calls("Create")); got != 1 {
		t.Errorf("Create calls after an automatic tick = %d, want 1: no auto-retry", got)
	}

	f.reset(testAt, runner.ProcessRunner{})
	if err := f.Store.QueueVerbIntent(t.Context(), f.Ticket.URL, "retry-push", testAt.Add(time.Second)); err != nil {
		t.Fatalf("QueueVerbIntent: %v", err)
	}
	f.tick(t)
	if got := len(f.Forge.Calls("Create")); got != 2 {
		t.Fatalf("Create calls after retry-push = %d, want 2", got)
	}
	tips, err = f.Store.LastPushedTips(t.Context())
	if err != nil {
		t.Fatalf("LastPushedTips: %v", err)
	}
	if tips[plan.TicketURL(f.Ticket.URL)] == "" {
		t.Error("pushes row missing after a successful retry-push")
	}

	pending, err := f.Store.PendingVerbIntents(t.Context(), "retry-push")
	if err != nil {
		t.Fatalf("PendingVerbIntents: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("pending retry-push intents = %+v, want none: consumed", pending)
	}
}

func TestPushPushableSkipsATicketWhoseBranchWasRemoved(t *testing.T) {
	t.Parallel()
	f := newPushFixture(t)
	gittest.Commit(t, f.Worktree, "agent.txt", "agent was here\n")
	gittest.Run(t, f.Repo, "push", "-q", "origin", "cc-1")
	dispositionAsPushed(t, f.Store, f.Ticket.URL, testAt)
	// Already fully pushed and recorded, mirroring a merged/base_gone row that has since been
	// torn down: RecordPush's own tip must match, or PushPlan would select it regardless.
	tip := f.head(t, f.Repo, "refs/heads/cc-1")
	push := storepkg.Push{
		Ticket: plan.TicketURL(f.Ticket.URL), Tip: plan.SHA(tip), BaseSHA: plan.SHA(tip), BaseBranch: "main", At: testAt,
	}
	if err := f.Store.RecordPush(t.Context(), push); err != nil {
		t.Fatalf("RecordPush: %v", err)
	}

	// The worktree and the branch are both gone -- tp remove --force's real effect.
	gittest.Run(t, f.Repo, "worktree", "remove", "--force", f.Worktree)
	gittest.Run(t, f.Repo, "branch", "-D", "cc-1")
	delete(f.Obs.Worktrees, branchRef("repo", "cc-1"))
	f.tick(t)
	// A second tick too: the hazard is that every future tick would fail, not just the first.
	f.tick(t)
}
