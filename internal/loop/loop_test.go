package loop_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/gh"
	"github.com/O-Marsters-1997/command-center/internal/gh/ghtest"
	"github.com/O-Marsters-1997/command-center/internal/gittest"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/option"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
	"github.com/O-Marsters-1997/command-center/internal/runner/runnertest"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/tp/tptest"
	"github.com/O-Marsters-1997/command-center/internal/tracker"
	"github.com/O-Marsters-1997/command-center/internal/tracker/trackertest"
	"github.com/O-Marsters-1997/command-center/internal/web"
	"github.com/O-Marsters-1997/command-center/internal/web/webtest"
)

type manualClock struct {
	mu      sync.Mutex
	now     time.Time
	waiters []manualWaiter
}

type manualWaiter struct {
	at time.Time
	ch chan time.Time
}

func newManualClock(at time.Time) *manualClock { return &manualClock{now: at} }

func (c *manualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *manualClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	c.waiters = append(c.waiters, manualWaiter{at: c.now.Add(d), ch: ch})
	return ch
}

func (c *manualClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	pending := c.waiters[:0]
	for _, w := range c.waiters {
		if w.at.After(c.now) {
			pending = append(pending, w)
			continue
		}
		w.ch <- c.now
	}
	c.waiters = pending
}

type frozenClock struct{ at time.Time }

func (c frozenClock) Now() time.Time                     { return c.at }
func (frozenClock) After(time.Duration) <-chan time.Time { return nil }

func fixedClock(at time.Time) loop.Clock { return frozenClock{at} }

func runLoop(ctx context.Context, t *testing.T, lp *loop.Loop) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- lp.Run(ctx) }()
	return func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("Run() = %v, want nil on cancellation", err)
		}
	}
}

func observing(obs plan.Observation) loop.ObserveFunc {
	return func(context.Context) (plan.Observation, error) { return obs, nil }
}

func TestLoop_RunOnce(t *testing.T) {
	t.Parallel()

	t.Run("records the observation at the injected clock", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		store := openStore(t)
		ref := branchRef("cc-sandbox", "cc-1-first")
		observed := plan.Observation{
			PRs:       map[plan.BranchRef]plan.PR{ref: {Number: 41, HeadRef: "cc-1-first", State: plan.Open}},
			Worktrees: map[plan.BranchRef]string{ref: "/tmp/cc-1-first"},
		}
		lp := newLoop(store, observing(observed), fixedClock(testAt), config.Config{}, config.Workspace{},
			runner.ProcessRunner{})
		if err := lp.RunOnce(ctx); err != nil {
			t.Fatalf("RunOnce() error = %v", err)
		}

		got, ok, err := store.LastObservation(ctx)
		if err != nil || !ok {
			t.Fatalf("LastObservation() = _, %v, %v, want an observation", ok, err)
		}
		if !got.ObservedAt.Equal(testAt) {
			t.Errorf("LastObservation().ObservedAt = %s, want %s", got.ObservedAt, testAt)
		}
		if got.PRs[ref].State != plan.Open {
			t.Errorf("LastObservation().PRs[%v].State = %v, want open", ref, got.PRs[ref].State)
		}
	})

	t.Run("applies queued launch intents", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		store := openStore(t)
		ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"}
		if err := store.UpsertTickets(ctx, []storepkg.Ticket{ticket}); err != nil {
			t.Fatalf("UpsertTickets: %v", err)
		}
		launch := storepkg.LaunchRequest{Ticket: "sandbox://CC-1", PromptHash: "hash-1", Group: "group-a", At: testAt}
		if err := store.QueueLaunchIntent(ctx, launch); err != nil {
			t.Fatalf("QueueLaunchIntent: %v", err)
		}

		lp := newLoop(store, observing(plan.Observation{}), fixedClock(testAt), config.Config{}, config.Workspace{},
			runner.ProcessRunner{})
		if err := lp.RunOnce(ctx); err != nil {
			t.Fatalf("RunOnce() error = %v", err)
		}

		memberships, err := store.LaunchMemberships(ctx)
		if err != nil {
			t.Fatalf("LaunchMemberships: %v", err)
		}
		if memberships["sandbox://CC-1"].LaunchID == 0 {
			t.Error("LaunchMemberships()[sandbox://CC-1].LaunchID = 0, want the queued launch applied")
		}
	})

	t.Run("a failed observe changes nothing but records the error", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		store := openStore(t)
		good := testAt
		bad := good.Add(15 * time.Second)
		ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"}
		if err := store.UpsertTickets(ctx, []storepkg.Ticket{ticket}); err != nil {
			t.Fatalf("UpsertTickets: %v", err)
		}
		observed := plan.Observation{
			PRs: map[plan.BranchRef]plan.PR{branchRef("cc-sandbox", "cc-1-first"): {Number: 41, State: plan.Open}},
		}
		ok := newLoop(store, observing(observed), fixedClock(good), config.Config{}, config.Workspace{},
			runner.ProcessRunner{})
		if err := ok.RunOnce(ctx); err != nil {
			t.Fatalf("first RunOnce() error = %v", err)
		}
		launch := storepkg.LaunchRequest{Ticket: "sandbox://CC-1", PromptHash: "hash-1", Group: "group-a", At: bad}
		if err := store.QueueLaunchIntent(ctx, launch); err != nil {
			t.Fatalf("QueueLaunchIntent: %v", err)
		}

		boom := errors.New("gh pr list: exit status 1")
		failing := newLoop(store, func(context.Context) (plan.Observation, error) {
			return plan.Observation{}, boom
		}, fixedClock(bad), config.Config{}, config.Workspace{}, runner.ProcessRunner{})
		if err := failing.RunOnce(ctx); !errors.Is(err, boom) {
			t.Fatalf("RunOnce(failing observe) error = %v, want %v", err, boom)
		}

		memberships, err := store.LaunchMemberships(ctx)
		if err != nil {
			t.Fatalf("LaunchMemberships: %v", err)
		}
		if len(memberships) != 0 {
			t.Errorf("LaunchMemberships() = %+v, want none applied by a failed observe", memberships)
		}
		after, found, err := store.LastObservation(ctx)
		if err != nil || !found {
			t.Fatalf("LastObservation() = _, %v, %v", found, err)
		}
		if !after.ObservedAt.Equal(good) || len(after.PRs) != 1 {
			t.Errorf("LastObservation() = at %s with %d PRs, want the good tick's at %s with 1", after.ObservedAt,
				len(after.PRs), good)
		}
		last, err := store.LastError(ctx)
		if err != nil || last.IsNone() {
			t.Fatalf("LastError() = %v, %v, want an error", last, err)
		}
		if tickErr := last.Unwrap(); !tickErr.At.Equal(bad) {
			t.Errorf("LastError().At = %s, want %s", tickErr.At, bad)
		}
		events, err := store.Events(ctx)
		if err != nil {
			t.Fatalf("Events: %v", err)
		}
		type kindAt struct {
			Kind storepkg.EventKind
			At   time.Time
		}
		var got []kindAt
		for _, e := range events {
			got = append(got, kindAt{e.Kind, e.At})
		}
		want := []kindAt{{"pr_opened", good}, {storepkg.EventTickError, bad}}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Events() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("checking ticks only advance on a successful observe", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		store := openStore(t)
		ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"}
		if err := store.UpsertTickets(ctx, []storepkg.Ticket{ticket}); err != nil {
			t.Fatalf("UpsertTickets: %v", err)
		}
		ok := newLoop(store, observing(plan.Observation{}), fixedClock(testAt), config.Config{}, config.Workspace{},
			runner.ProcessRunner{})
		failing := newLoop(store, func(context.Context) (plan.Observation, error) {
			return plan.Observation{}, errors.New("gh pr list: exit status 1")
		}, fixedClock(testAt.Add(time.Hour)), config.Config{}, config.Workspace{}, runner.ProcessRunner{})

		for range 3 {
			if err := ok.RunOnce(ctx); err != nil {
				t.Fatalf("RunOnce() error = %v", err)
			}
		}
		for range 50 {
			if err := failing.RunOnce(ctx); err == nil {
				t.Fatal("RunOnce(failing observe) error = nil, want one")
			}
		}
		if err := ok.RunOnce(ctx); err != nil {
			t.Fatalf("RunOnce() error = %v", err)
		}

		ticks, err := store.CheckingTicks(ctx)
		if err != nil {
			t.Fatalf("CheckingTicks: %v", err)
		}
		if got := ticks["sandbox://CC-1"]; got != 4 {
			t.Errorf("CheckingTicks()[sandbox://CC-1] = %d after 4 good and 50 failed observes, want 4", got)
		}
	})

	t.Run("never spawns past max_agents across a verb and an automatic launch", func(t *testing.T) {
		t.Parallel()
		root, repoPath := repoWithOrigin(t)
		worktreePath := cutWorktree(t, repoPath, "cc-1")
		store := openStore(t)
		tickets := []storepkg.Ticket{
			{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"},
			{URL: "sandbox://CC-2", Repo: "repo", Branch: "cc-2"},
		}
		if err := store.UpsertTickets(t.Context(), tickets); err != nil {
			t.Fatalf("UpsertTickets: %v", err)
		}
		dispositionAsPushed(t, store, tickets[0].URL, testAt)
		authoriseTicket(t, store, tickets[1].URL, composeHash(tickets[1].URL), testAt)
		if err := store.QueueVerbIntent(t.Context(), storepkg.VerbRequest{
			Ticket: tickets[0].URL, Verb: "re-run", At: testAt,
		}); err != nil {
			t.Fatalf("QueueVerbIntent(re-run): %v", err)
		}

		obs := plan.Observation{Worktrees: map[plan.BranchRef]string{branchRef("repo", "cc-1"): worktreePath}}
		cfg, ws := testConfigAndWorkspace(t, root, 1, []string{"true"})
		fake := &runnertest.Runner{}
		lp := newLoop(store, observing(obs), fixedClock(testAt.Add(time.Minute)), cfg, ws, fake,
			loop.WithForge(&ghtest.Forge{}), loop.WithWorktrees(&tptest.Worktrees{}))
		if err := lp.RunOnce(t.Context()); err != nil {
			t.Fatalf("RunOnce() error = %v", err)
		}

		if got := len(fake.Spawns()); got != 1 {
			t.Fatalf("spawns = %d, want 1 (max_agents = 1)", got)
		}
		latest, err := store.LatestRunsByTicket(t.Context())
		if err != nil {
			t.Fatalf("LatestRunsByTicket(): %v", err)
		}
		if _, ran := latest[plan.TicketURL(tickets[1].URL)]; ran {
			t.Error("LatestRunsByTicket() has sandbox://CC-2, want it held back by max_agents")
		}
	})

	t.Run("a run disposed this tick is pushed in the same tick", func(t *testing.T) {
		t.Parallel()
		root, repoPath := repoWithOrigin(t)
		forge := &ghtest.Forge{}
		worktreePath := cutWorktree(t, repoPath, "cc-1")
		baseline := gittest.Run(t, repoPath, "rev-parse", "refs/heads/cc-1")
		gittest.Commit(t, worktreePath, "agent.txt", "agent was here\n")
		store := openStore(t)
		ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
		if err := store.UpsertTickets(t.Context(), []storepkg.Ticket{ticket}); err != nil {
			t.Fatalf("UpsertTickets: %v", err)
		}
		runID, err := store.InsertRunSkeleton(t.Context(), storepkg.RunSkeleton{
			Ticket: ticket.URL, Kind: "agent", BaselineSHA: baseline, PromptHash: "hash-1",
		})
		if err != nil {
			t.Fatalf("InsertRunSkeleton: %v", err)
		}
		spawn := storepkg.Spawn{
			RunID: plan.RunID(runID), Pgid: 4242, StartedAt: testAt, LogPath: filepath.Join(t.TempDir(), "1.jsonl"),
		}
		if err := store.RecordSpawn(t.Context(), spawn); err != nil {
			t.Fatalf("RecordSpawn: %v", err)
		}

		obs := plan.Observation{Worktrees: map[plan.BranchRef]string{branchRef("repo", "cc-1"): worktreePath}}
		cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
		lp := newLoop(store, observing(obs), fixedClock(testAt.Add(time.Minute)), cfg, ws, &runnertest.Runner{},
			loop.WithForge(forge))
		if err := lp.RunOnce(t.Context()); err != nil {
			t.Fatalf("RunOnce() error = %v", err)
		}

		if !gittest.HasRef(t, filepath.Join(root, "remote.git"), "refs/heads/cc-1") {
			t.Error("origin has no cc-1, want the run disposed this tick pushed")
		}
		if got := len(forge.Calls("Create")); got != 1 {
			t.Errorf("Create calls = %d, want 1", got)
		}
	})

	t.Run("prunes tick errors past retention", func(t *testing.T) {
		t.Parallel()
		f := newLoopFixture(t)
		stale := storepkg.Event{At: testAt.Add(-31 * 24 * time.Hour), Kind: storepkg.EventTickError, Detail: "old"}
		recent := storepkg.Event{At: testAt.Add(-time.Hour), Kind: storepkg.EventTickError, Detail: "new"}
		for _, e := range []storepkg.Event{stale, recent} {
			if err := f.Store.AppendEvent(t.Context(), e); err != nil {
				t.Fatalf("AppendEvent(%s): %v", e.Kind, err)
			}
		}

		f.Tick(t)

		events, err := f.Store.Events(t.Context())
		if err != nil {
			t.Fatalf("Events(): %v", err)
		}
		var details []string
		for _, e := range events {
			if e.Kind == storepkg.EventTickError {
				details = append(details, e.Detail)
			}
		}
		if diff := cmp.Diff([]string{"new"}, details); diff != "" {
			t.Errorf("tick_error details mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("a run that wrote its question file is disposed as asked, even with commits", func(t *testing.T) {
		t.Parallel()
		root, repoPath := repoWithOrigin(t)
		worktreePath := filepath.Join(t.TempDir(), "wt")
		gittest.Run(t, repoPath, "worktree", "add", "-b", "cc-1", worktreePath, "origin/main")
		baseline := gittest.Run(t, repoPath, "rev-parse", "refs/heads/cc-1")
		gittest.Run(t, worktreePath, "commit", "-q", "--allow-empty", "-m", "agent work")
		store := openStore(t)
		ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
		if err := store.UpsertTickets(t.Context(), []storepkg.Ticket{ticket}); err != nil {
			t.Fatalf("UpsertTickets: %v", err)
		}
		runID, err := store.InsertRunSkeleton(t.Context(), storepkg.RunSkeleton{
			Ticket: ticket.URL, Kind: "agent", BaselineSHA: baseline, PromptHash: "hash-1",
		})
		if err != nil {
			t.Fatalf("InsertRunSkeleton: %v", err)
		}
		spawn := storepkg.Spawn{RunID: plan.RunID(runID), Pgid: 999, StartedAt: testAt, LogPath: "/state/runs/1.jsonl"}
		if err := store.RecordSpawn(t.Context(), spawn); err != nil {
			t.Fatalf("RecordSpawn: %v", err)
		}
		cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
		long := "Which database?\n" + strings.Repeat("x", 5000)
		questionPath := filepath.Join(ws.RunsDir, fmt.Sprintf("%d.question", runID))
		if err := os.WriteFile(questionPath, []byte(long), 0o600); err != nil {
			t.Fatalf("WriteFile(%q): %v", questionPath, err)
		}

		obs := plan.Observation{Worktrees: map[plan.BranchRef]string{branchRef("repo", "cc-1"): worktreePath}}
		fake := &runnertest.Runner{}
		fake.Exit(999, 0)
		lp := newLoop(store, observing(obs), fixedClock(testAt.Add(30*time.Second)), cfg, ws, fake)
		if err := lp.RunOnce(t.Context()); err != nil {
			t.Fatalf("RunOnce() error = %v", err)
		}

		latest, err := store.LatestRunsByTicket(t.Context())
		if err != nil {
			t.Fatalf("LatestRunsByTicket(): %v", err)
		}
		summary := latest[plan.TicketURL(ticket.URL)]
		if summary.Outcome != option.Some(plan.OutcomeAsked) {
			t.Fatalf("outcome = %v, want asked", summary.Outcome)
		}
		if !strings.HasPrefix(summary.Question, "Which database?\n") || len(summary.Question) != 4096 {
			t.Errorf("question = %d bytes, want 4096 starting with the agent's question", len(summary.Question))
		}
		if got := gittest.Run(t, repoPath, "ls-remote", "origin", "refs/heads/cc-1"); got != "" {
			t.Errorf("ls-remote origin cc-1 = %q, want nothing pushed", got)
		}
	})

	t.Run("drives an imported ticket from ready to review_me", func(t *testing.T) {
		t.Parallel()
		root, _ := repoWithOrigin(t)
		cfg, ws := testConfigAndWorkspace(t, root, 1, []string{"true"})
		pushSettingsFile(t, config.CheckoutPath(root, "repo"), "[checks]\nsuccess = \"CI\"\n")
		issue := tracker.Ticket{URL: "https://github.com/acme/repo/issues/1", Number: 1, Title: "Add x"}
		source := &trackertest.Source{ByFeature: map[string][]tracker.Ticket{"project:x": {issue}}}
		resolve := func(tracker.Kind, string) (tracker.Source, error) { return source, nil }
		store := openStore(t)
		trackRepo(t, store, "repo", "git@github.com:acme/repo.git")
		at := testAt.AddDate(0, 2, -15)
		forge := &ghtest.Forge{}
		worktrees := &tptest.Worktrees{}
		fake := &runnertest.Runner{}
		lp := newLoopWithDeps(loop.Deps{
			Store: store, Observe: loop.NewObserver(store, forge, cfg), Clock: fixedClock(at), Config: cfg, Workspace: ws,
			Runner: fake, Tracker: resolve,
		}, loop.WithForge(forge), loop.WithWorktrees(worktrees))
		if err := store.QueueVerbIntent(t.Context(), storepkg.VerbRequest{
			Ticket: "project:x", Verb: plan.VerbImport, At: at,
		}); err != nil {
			t.Fatalf("QueueVerbIntent(import): %v", err)
		}
		authoriseTicket(t, store, issue.URL, composeHash(issue.URL), at)
		tick := func(n int) {
			t.Helper()
			for range n {
				if err := lp.RunOnce(t.Context()); err != nil {
					t.Fatalf("RunOnce() error = %v", err)
				}
			}
		}

		tick(1)
		spawns := fake.Spawns()
		if len(spawns) != 1 {
			t.Fatalf("spawns = %d, want the imported ticket's implement run", len(spawns))
		}
		gittest.Commit(t, spawns[0].WorktreePath, "x.go", "package x\n")
		fake.Exit(1, 0)
		tick(4)
		cut := worktrees.Cut()
		if len(forge.Calls("Create")) != 1 || len(cut) != 1 {
			t.Fatalf("Create calls = %+v, cut = %v, want one of each", forge.Calls("Create"), cut)
		}
		branch := cut[0]
		forge.SetSnapshot(gh.Snapshot{ByBranch: map[string]gh.PR{branch: {
			Number: 1, HeadRef: branch, State: gh.Open,
			HeadOid: gittest.Run(t, filepath.Join(root, "remote.git"), "rev-parse", "refs/heads/"+branch),
			Checks:  map[string]gh.CheckState{"CI": {Status: "COMPLETED", Conclusion: "SUCCESS"}},
		}}})
		tick(2)

		if got := len(fake.Spawns()); got != 1 {
			t.Errorf("spawns = %d, want only the implement run", got)
		}
		row := webtest.RowFor(t, renderPage(t, openServer(store, fixedClock(at), "")), issue.URL)
		if row.State != "review_me" {
			t.Errorf("row state = %q, want review_me", row.State)
		}
	})
}

func TestLoop_Run(t *testing.T) {
	t.Parallel()

	t.Run("ticks on the injected clock without sleeping", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		synctest.Test(t, func(t *testing.T) {
			var ticks atomic.Int32
			observe := func(context.Context) (plan.Observation, error) {
				ticks.Add(1)
				return plan.Observation{}, nil
			}
			clock := newManualClock(testAt)
			lp := newLoop(store, observe, clock, config.Config{}, config.Workspace{}, runner.ProcessRunner{})
			stop := runLoop(t.Context(), t, lp)

			synctest.Wait()
			if got := ticks.Load(); got != 1 {
				t.Fatalf("ticks after start = %d, want 1", got)
			}
			for want := int32(2); want <= 4; want++ {
				clock.Advance(15 * time.Second)
				synctest.Wait()
				if got := ticks.Load(); got != want {
					t.Fatalf("ticks after advancing the clock = %d, want %d", got, want)
				}
			}
			stop()
		})
	})

	t.Run("a nudge ticks at once and mid-tick nudges coalesce", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		synctest.Test(t, func(t *testing.T) {
			var ticks atomic.Int32
			proceed := make(chan struct{})
			var closeProceed sync.Once
			t.Cleanup(func() { closeProceed.Do(func() { close(proceed) }) })
			observe := func(context.Context) (plan.Observation, error) {
				if ticks.Add(1) == 2 {
					<-proceed
				}
				return plan.Observation{}, nil
			}
			lp := newLoop(store, observe, newManualClock(testAt), config.Config{}, config.Workspace{},
				runner.ProcessRunner{})
			nudger := lp.Nudger()
			stop := runLoop(t.Context(), t, lp)

			synctest.Wait()
			if got := ticks.Load(); got != 1 {
				t.Fatalf("ticks after start = %d, want 1", got)
			}
			nudger.Nudge()
			synctest.Wait()
			if got := ticks.Load(); got != 2 {
				t.Fatalf("ticks after a nudge = %d, want 2", got)
			}
			for range 5 {
				nudger.Nudge()
			}
			closeProceed.Do(func() { close(proceed) })
			synctest.Wait()
			if got := ticks.Load(); got != 3 {
				t.Errorf("ticks after five mid-tick nudges = %d, want 3", got)
			}
			stop()
		})
	})

	t.Run("a verb handler's nudge reaches the loop", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		ticks := make(chan struct{}, 8)
		observe := func(context.Context) (plan.Observation, error) {
			ticks <- struct{}{}
			return plan.Observation{}, nil
		}
		clock := newManualClock(testAt)
		lp := newLoop(store, observe, clock, config.Config{}, config.Workspace{}, runner.ProcessRunner{})
		server := web.NewServer(web.Deps{Logger: discardLog, Store: store, Clock: clock, Nudger: lp.Nudger()})
		stop := runLoop(t.Context(), t, lp)
		t.Cleanup(stop)
		<-ticks

		req := httptest.NewRequest(http.MethodPost, "/features/"+url.PathEscape("project:x")+"/import", nil)
		req.Header.Set("HX-Request", "true")
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("POST import status = %d, want 200: %s", rec.Code, rec.Body)
		}

		<-ticks
	})
}
