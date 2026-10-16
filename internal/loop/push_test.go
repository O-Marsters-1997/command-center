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
	"github.com/O-Marsters-1997/command-center/internal/tp/tptest"
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
	if err := store.RecordDisposition(t.Context(), storepkg.Disposition{
		RunID: plan.RunID(runID), Outcome: plan.OutcomePush, ExitCode: option.Some(0),
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

func (f *pushFixture) pushedTip(t *testing.T) plan.SHA {
	t.Helper()
	tips, err := f.Store.LastPushedTips(t.Context())
	if err != nil {
		t.Fatalf("LastPushedTips: %v", err)
	}
	return tips[plan.TicketURL(f.Ticket.URL)]
}

func TestPush(t *testing.T) {
	t.Parallel()

	t.Run("pushes and opens one PR, then stays idempotent", func(t *testing.T) {
		t.Parallel()
		f := newPushFixture(t)
		gittest.Commit(t, f.Worktree, "agent.txt", "agent was here\n")
		dispositionAsPushed(t, f.Store, f.Ticket.URL, testAt)
		f.tick(t)

		if !gittest.HasRef(t, f.Origin, "refs/heads/cc-1") {
			t.Fatal("origin has no cc-1, want it pushed")
		}
		calls := f.Forge.Calls("Create")
		if len(calls) != 1 || calls[0].Repo != f.Worktree || calls[0].Base != "main" || calls[0].Draft {
			t.Errorf("Create calls = %+v, want one non-draft PR on main from the worktree", calls)
		}
		if f.pushedTip(t) == "" {
			t.Error("LastPushedTips() has no tip, want the push recorded")
		}

		f.tick(t)
		if got := len(f.Forge.Calls("Create")); got != 1 {
			t.Errorf("Create calls after a second tick = %d, want 1", got)
		}
	})

	t.Run("refuses a policy hit, never pushes and never retries", func(t *testing.T) {
		t.Parallel()
		f := newPushFixture(t)
		gittest.Commit(t, f.Worktree, ".github/workflows/x.yml", "name: x\n")
		dispositionAsPushed(t, f.Store, f.Ticket.URL, testAt)
		f.tick(t)

		if gittest.HasRef(t, f.Origin, "refs/heads/cc-1") {
			t.Error("origin has cc-1, want nothing pushed")
		}
		facts, err := f.Store.PushFacts(t.Context())
		if err != nil {
			t.Fatalf("PushFacts: %v", err)
		}
		if fact := facts[plan.TicketURL(f.Ticket.URL)]; !fact.Refused || fact.RefusedPath != ".github/workflows/x.yml" {
			t.Errorf("PushFacts() = %+v, want refused at .github/workflows/x.yml", fact)
		}
		f.tick(t)
		if got := f.events(t, "push_refused"); len(got) != 1 || got[0].TicketURL != f.Ticket.URL {
			t.Errorf("push_refused events after two ticks = %+v, want one for %s", got, f.Ticket.URL)
		}
	})

	t.Run("adopts an existing open PR rather than duplicating it", func(t *testing.T) {
		t.Parallel()
		f := newPushFixture(t)
		gittest.Commit(t, f.Worktree, "agent.txt", "agent was here\n")
		dispositionAsPushed(t, f.Store, f.Ticket.URL, testAt)
		f.Obs.PRs[branchRef("repo", "cc-1")] = plan.PR{Number: 7, HeadRef: "cc-1", State: plan.Open}
		f.tick(t)

		if !gittest.HasRef(t, f.Origin, "refs/heads/cc-1") {
			t.Error("origin has no cc-1, want it pushed")
		}
		if got := len(f.Forge.Calls("Create")); got != 0 {
			t.Errorf("Create calls = %d, want 0", got)
		}
		if f.pushedTip(t) == "" {
			t.Error("LastPushedTips() has no tip, want the push recorded")
		}
	})

	t.Run("a failed PR create is not retried until retry-push", func(t *testing.T) {
		t.Parallel()
		f := newPushFixture(t)
		gittest.Commit(t, f.Worktree, "agent.txt", "agent was here\n")
		dispositionAsPushed(t, f.Store, f.Ticket.URL, testAt)
		f.reset(testAt, runner.ProcessRunner{}, loop.WithForge(failingCreate{f.Forge}))
		f.tick(t)

		if !gittest.HasRef(t, f.Origin, "refs/heads/cc-1") {
			t.Fatal("origin has no cc-1, want the branch pushed though the PR create failed")
		}
		facts, err := f.Store.PushFacts(t.Context())
		if err != nil {
			t.Fatalf("PushFacts: %v", err)
		}
		if !facts[plan.TicketURL(f.Ticket.URL)].Failed || f.pushedTip(t) != "" {
			t.Fatalf("after a failed create: failed = %v, tip = %q, want failed and no tip",
				facts[plan.TicketURL(f.Ticket.URL)].Failed, f.pushedTip(t))
		}
		f.tick(t)
		if got := len(f.Forge.Calls("Create")); got != 1 {
			t.Errorf("Create calls after an automatic tick = %d, want 1", got)
		}

		f.reset(testAt, runner.ProcessRunner{})
		if err := f.Store.QueueVerbIntent(t.Context(), storepkg.VerbRequest{
			Ticket: f.Ticket.URL, Verb: "retry-push", At: testAt.Add(time.Second),
		}); err != nil {
			t.Fatalf("QueueVerbIntent(retry-push): %v", err)
		}
		f.tick(t)

		if got := len(f.Forge.Calls("Create")); got != 2 {
			t.Fatalf("Create calls after retry-push = %d, want 2", got)
		}
		if f.pushedTip(t) == "" {
			t.Error("LastPushedTips() has no tip after a successful retry-push")
		}
		pending, err := f.Store.PendingVerbIntents(t.Context(), "retry-push")
		if err != nil {
			t.Fatalf("PendingVerbIntents: %v", err)
		}
		if len(pending) != 0 {
			t.Errorf("PendingVerbIntents(retry-push) = %+v, want none", pending)
		}
	})

	t.Run("a ticket whose branch was removed ticks without error", func(t *testing.T) {
		t.Parallel()
		f := newPushFixture(t)
		gittest.Commit(t, f.Worktree, "agent.txt", "agent was here\n")
		gittest.Run(t, f.Repo, "push", "-q", "origin", "cc-1")
		dispositionAsPushed(t, f.Store, f.Ticket.URL, testAt)
		tip := f.head(t, f.Repo, "refs/heads/cc-1")
		push := storepkg.Push{
			Ticket: plan.TicketURL(f.Ticket.URL), Tip: plan.SHA(tip), BaseSHA: plan.SHA(tip), BaseBranch: "main", At: testAt,
		}
		if err := f.Store.RecordPush(t.Context(), push); err != nil {
			t.Fatalf("RecordPush: %v", err)
		}
		gittest.Run(t, f.Repo, "worktree", "remove", "--force", f.Worktree)
		gittest.Run(t, f.Repo, "branch", "-D", "cc-1")
		delete(f.Obs.Worktrees, branchRef("repo", "cc-1"))

		f.tick(t)
		f.tick(t)
	})

	t.Run("an unreadable settings file skips the repo and records the error", func(t *testing.T) {
		t.Parallel()
		f := newPushFixture(t, loop.WithWorktrees(&tptest.Worktrees{}))
		gittest.Commit(t, f.Worktree, "agent.txt", "agent was here\n")
		dispositionAsPushed(t, f.Store, f.Ticket.URL, testAt)
		f.Obs.SettingsErrors = map[string]string{"repo": "line 2: unknown key \"stackin\""}
		f.tick(t)

		if gittest.HasRef(t, f.Origin, "refs/heads/cc-1") {
			t.Error("origin has cc-1, want nothing pushed for a repo whose settings can't be read")
		}
		lastErr, err := f.Store.LastError(t.Context())
		if err != nil || lastErr.IsNone() {
			t.Fatalf("LastError() = %v, %v, want an error", lastErr, err)
		}
	})
}
