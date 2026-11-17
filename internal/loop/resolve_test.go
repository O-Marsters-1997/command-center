package loop_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/adapter/git/gittest"
	"github.com/O-Marsters-1997/command-center/internal/adapter/runner/runnertest"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web/webtest"
)

func (c conflictFixture) queueResolve(t *testing.T) {
	t.Helper()
	if err := c.store.QueueVerbIntent(t.Context(), storepkg.VerbRequest{
		Ticket: c.f.child.URL, Verb: plan.VerbResolve, At: testAt.Add(time.Hour),
	}); err != nil {
		t.Fatalf("QueueVerbIntent(resolve): %v", err)
	}
}

func conflictedPair(t *testing.T, store *storepkg.Store) string {
	t.Helper()
	events, err := store.Events(t.Context())
	if err != nil {
		t.Fatalf("Events(): %v", err)
	}
	for _, e := range events {
		if e.Kind == storepkg.EventRefreshConflicted {
			fields := strings.SplitN(e.Detail, " ", 3)
			return fields[0] + " " + fields[1]
		}
	}
	t.Fatal("Events() has no refresh_conflicted event")
	return ""
}

func TestResolve(t *testing.T) {
	t.Parallel()

	t.Run("launches an agent in the mid-merge worktree with the facts", func(t *testing.T) {
		t.Parallel()
		root, repoPath := repoWithOrigin(t)
		fake := &runnertest.Runner{}
		c := newConflictFixtureWith(t, root, repoPath, testAt, fake, nil)
		c.tick(t)
		c.tick(t)
		pair := conflictedPair(t, c.store)
		c.queueResolve(t)
		c.tick(t)

		spawns := fake.Spawns()
		if len(spawns) != 1 {
			t.Fatalf("spawns = %d, want 1", len(spawns))
		}
		spawn := spawns[0]
		if spawn.WorktreePath != c.f.childWorktree {
			t.Errorf("spawn worktree = %q, want %q", spawn.WorktreePath, c.f.childWorktree)
		}
		for _, want := range []string{
			"shared.txt", "Add the lobby", "Rewrite the frontdoor", "origin/parent",
			strings.Fields(pair)[0], strings.Fields(pair)[1], plan.ResolveCommitPrefix,
		} {
			if !strings.Contains(spawn.Prompt, want) {
				t.Errorf("spawn prompt = %q, want it to mention %q", spawn.Prompt, want)
			}
		}
		latest, err := c.store.LatestRunsByTicket(t.Context())
		if err != nil {
			t.Fatalf("LatestRunsByTicket(): %v", err)
		}
		if kind := latest[plan.TicketURL(c.f.child.URL)].Kind; kind != plan.RunKindResolve {
			t.Errorf("run kind = %q, want %q", kind, plan.RunKindResolve)
		}
		if !hasEventIn(t, c.store, storepkg.EventResolveStarted, pair) {
			t.Errorf("Events() has no resolve_started for %q", pair)
		}
	})

	t.Run("refuses a second resolve at the same pair of tips", func(t *testing.T) {
		t.Parallel()
		root, repoPath := repoWithOrigin(t)
		fake := &runnertest.Runner{}
		c := newConflictFixtureWith(t, root, repoPath, testAt, fake, nil)
		c.tick(t)
		c.tick(t)
		pair := conflictedPair(t, c.store)
		if err := c.store.AppendEvent(t.Context(), storepkg.Event{
			At: testAt, TicketURL: c.f.child.URL, Kind: storepkg.EventResolveStarted, Detail: pair,
		}); err != nil {
			t.Fatalf("AppendEvent(resolve_started): %v", err)
		}
		c.queueResolve(t)
		c.tick(t)

		if got := len(fake.Spawns()); got != 0 {
			t.Errorf("spawns = %d, want 0", got)
		}
		if !hasEventIn(t, c.store, storepkg.EventResolveRefused, "already ran") {
			t.Error("Events() has no resolve_refused saying a resolve already ran")
		}
	})

	t.Run("a failing verify_command after the commit lands in verification failed", func(t *testing.T) {
		t.Parallel()
		root, repoPath := repoWithOrigin(t)
		fake := &runnertest.Runner{}
		c := newConflictFixtureWith(t, root, repoPath, testAt, fake, []string{"false"})
		c.tick(t)
		c.tick(t)
		c.queueResolve(t)
		c.tick(t)

		if err := os.WriteFile(filepath.Join(c.f.childWorktree, "shared.txt"), []byte("both lines\n"), 0o600); err != nil {
			t.Fatalf("WriteFile(shared.txt): %v", err)
		}
		gittest.Run(t, c.f.childWorktree, "add", "shared.txt")
		gittest.Run(t, c.f.childWorktree, "commit", "-q", "-m", plan.ResolveCommitPrefix+" keep both lines")
		fake.Exit(1, 0)
		c.tick(t)

		if !hasEventIn(t, c.store, storepkg.EventVerificationFailed, "") {
			t.Error("Events() has no verification_failed after the resolve commit")
		}
		c.tick(t)
		if row := webtest.RowFor(t, renderPage(t, c.server), c.f.child.URL); row.State != "Verification failed" {
			t.Errorf("child state = %q, want %q", row.State, "Verification failed")
		}
	})
}
