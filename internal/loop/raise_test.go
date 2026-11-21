package loop_test

import (
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/adapter/git/gittest"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
)

func (f *pushFixture) awaitingPR(t *testing.T) {
	t.Helper()
	f.seedAgentPush(t)
	f.Obs.Settings = map[string]plan.RepoSettings{"repo": {Raise: plan.RaiseManual}}
	f.tick(t)
}

func (f *pushFixture) raise(t *testing.T, tip string) int64 {
	t.Helper()
	id, err := f.Store.Raise(t.Context(), plan.TicketURL(f.Ticket.URL), plan.SHA(tip), testAt.Add(time.Second))
	if err != nil {
		t.Fatalf("Raise: %v", err)
	}
	return id
}

func TestRaise(t *testing.T) {
	t.Parallel()

	t.Run("pushes the reviewed tip and opens the PR", func(t *testing.T) {
		t.Parallel()
		f := newPushFixture(t)
		f.awaitingPR(t)
		if got := len(f.Forge.Calls("Create")); got != 0 {
			t.Fatalf("Create calls while awaiting PR = %d, want 0", got)
		}
		tip := f.head(t, f.Worktree, "HEAD")
		id := f.raise(t, tip)
		f.tick(t)

		if got := f.head(t, f.Origin, "refs/heads/cc-1"); got != tip {
			t.Errorf("origin cc-1 = %s, want the reviewed tip %s", got, tip)
		}
		if got := len(f.Forge.Calls("Create")); got != 1 {
			t.Errorf("Create calls after raise = %d, want 1", got)
		}
		consumed, err := f.Store.IntentConsumed(t.Context(), id)
		if err != nil || !consumed {
			t.Errorf("IntentConsumed(%d) = %v, %v, want true", id, consumed, err)
		}
	})

	t.Run("a stale tip pushes nothing and records the refusal", func(t *testing.T) {
		t.Parallel()
		f := newPushFixture(t)
		f.awaitingPR(t)
		reviewed := f.head(t, f.Worktree, "HEAD")
		gittest.Commit(t, f.Worktree, "later.txt", "a follow-up landed\n")
		id := f.raise(t, reviewed)
		f.tick(t)

		if gittest.HasRef(t, f.Origin, "refs/heads/cc-1") {
			t.Error("origin has cc-1, want nothing pushed for a stale raise")
		}
		if got := len(f.Forge.Calls("Create")); got != 0 {
			t.Errorf("Create calls after stale raise = %d, want 0", got)
		}
		if !hasEvent(storetest.Events(t, f.Store, storepkg.EventRaiseRefused), storepkg.EventRaiseRefused, "tip has moved") {
			t.Error("no raise_refused event naming the moved tip")
		}
		consumed, err := f.Store.IntentConsumed(t.Context(), id)
		if err != nil || !consumed {
			t.Errorf("IntentConsumed(%d) = %v, %v, want true", id, consumed, err)
		}
	})
}
