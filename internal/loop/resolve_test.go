package loop_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/adapter/git/gittest"
	"github.com/O-Marsters-1997/command-center/internal/adapter/runner/runnertest"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
	"github.com/O-Marsters-1997/command-center/internal/web/webtest"
)

func (c conflictFixture) queueResolve(t *testing.T) {
	t.Helper()
	storetest.Verb(t, c.store, storepkg.VerbRequest{
		Ticket: c.f.child.URL, Verb: plan.VerbResolve, At: testAt.Add(time.Hour),
	})
}

func conflictedPair(t *testing.T, store *storepkg.Store) string {
	t.Helper()
	conflicts := storetest.Events(t, store, storepkg.EventRefreshConflicted)
	if len(conflicts) == 0 {
		t.Fatal("Events() has no refresh_conflicted event")
	}
	fields := strings.SplitN(conflicts[0].Detail, " ", 3)
	return fields[0] + " " + fields[1]
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
		if !strings.Contains(spawn.Prompt, "shared.txt") {
			t.Errorf("spawn prompt = %q, want it to name the conflicted file", spawn.Prompt)
		}
		latest, err := c.store.LatestRunsByTicket(t.Context())
		if err != nil {
			t.Fatalf("LatestRunsByTicket(): %v", err)
		}
		if kind := latest[plan.TicketURL(c.f.child.URL)].Kind; kind != plan.RunKindResolve {
			t.Errorf("run kind = %q, want %q", kind, plan.RunKindResolve)
		}
		if !hasEvent(storetest.Events(t, c.store, ""), storepkg.EventResolveStarted, pair) {
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
		if !hasEvent(storetest.Events(t, c.store, ""), storepkg.EventResolveRefused, "already ran") {
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

		spawns := fake.Spawns()
		if len(spawns) != 2 || !slices.Equal(spawns[1].AgentCommand, []string{"false"}) {
			t.Fatalf("spawns = %+v, want the resolve then the verify command", spawns)
		}
		if row := webtest.RowFor(t, renderPage(t, c.server), c.f.child.URL); row.State != "Verifying" {
			t.Errorf("child state while verifying = %q, want Verifying", row.State)
		}
		fake.Exit(2, 1)
		c.tick(t)
		verifies, err := c.store.LatestVerifyRuns(t.Context())
		if err != nil {
			t.Fatalf("LatestVerifyRuns: %v", err)
		}
		if got := verifies[plan.TicketURL(c.f.child.URL)].Outcome; got != option.Some(plan.OutcomeFailed) {
			t.Errorf("verify outcome = %v, want failed", got)
		}
	})
}
