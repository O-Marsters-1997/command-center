package loop_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cctest"

	storepkg "github.com/O-Marsters-1997/command-center/internal/store"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
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

func TestRunOnceRecordsTheObservation(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	at := testAt

	observed := plan.Observation{
		PRs: map[plan.BranchRef]plan.PR{
			branchRef("cc-sandbox", "cc-1-first"): {Number: 41, HeadRef: "cc-1-first", State: plan.Open},
		},
		Worktrees: map[plan.BranchRef]string{branchRef("cc-sandbox", "cc-1-first"): "/tmp/cc-1-first"},
	}
	lp := newLoop(store,
		func(context.Context) (plan.Observation, error) { return observed, nil },
		cctest.Clock{At: at}, config.Config{}, config.Workspace{}, runner.ProcessRunner{})
	if err := lp.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	got, ok, err := store.LastObservation(ctx)
	if err != nil || !ok {
		t.Fatalf("LastObservation: ok=%v err=%v", ok, err)
	}
	if !got.ObservedAt.Equal(at) {
		t.Errorf("observed_at = %s, want the injected clock %s", got.ObservedAt, at)
	}
	if got.PRs[branchRef("cc-sandbox", "cc-1-first")].State != plan.Open {
		t.Errorf("prs = %+v, want cc-1-first open", got.PRs)
	}
}

func TestRunOnceAppliesQueuedLaunchIntents(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"}
	if err := store.UpsertTickets(ctx, []storepkg.Ticket{ticket}); err != nil {
		t.Fatalf("UpsertTickets: %v", err)
	}
	at := testAt
	launch := storepkg.LaunchRequest{Ticket: "sandbox://CC-1", PromptHash: "hash-1", Group: "group-a", At: at}
	if err := store.QueueLaunchIntent(ctx, launch); err != nil {
		t.Fatalf("QueueLaunchIntent: %v", err)
	}

	stub := func(context.Context) (plan.Observation, error) { return plan.Observation{}, nil }
	lp := newLoop(store, stub, cctest.Clock{At: at}, config.Config{}, config.Workspace{}, runner.ProcessRunner{})
	if err := lp.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	memberships, err := store.LaunchMemberships(ctx)
	if err != nil {
		t.Fatalf("LaunchMemberships: %v", err)
	}
	if memberships["sandbox://CC-1"].LaunchID == 0 {
		t.Error("RunOnce did not apply the queued launch intent after a successful observe")
	}
}

func TestRunOnceFailedObserveChangesNothing(t *testing.T) {
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
	ok := newLoop(store,
		func(context.Context) (plan.Observation, error) { return observed, nil },
		cctest.Clock{At: good}, config.Config{}, config.Workspace{}, runner.ProcessRunner{})
	if err := ok.RunOnce(ctx); err != nil {
		t.Fatalf("first RunOnce: %v", err)
	}

	launch := storepkg.LaunchRequest{Ticket: "sandbox://CC-1", PromptHash: "hash-1", Group: "group-a", At: bad}
	if err := store.QueueLaunchIntent(ctx, launch); err != nil {
		t.Fatalf("QueueLaunchIntent: %v", err)
	}

	boom := errors.New("gh pr list: exit status 1")
	failing := newLoop(store, func(context.Context) (plan.Observation, error) {
		return plan.Observation{}, boom
	}, cctest.Clock{At: bad}, config.Config{}, config.Workspace{}, runner.ProcessRunner{})

	err := failing.RunOnce(ctx)
	if err == nil {
		t.Fatal("RunOnce returned nil for a failed observe")
	}
	if !errors.Is(err, boom) {
		t.Errorf("error %v does not wrap the observe failure", err)
	}

	memberships, err := store.LaunchMemberships(ctx)
	if err != nil {
		t.Fatalf("LaunchMemberships: %v", err)
	}
	if len(memberships) != 0 {
		t.Errorf("memberships = %+v, want none: a failed observe must apply no queued intent", memberships)
	}

	after, found, err := store.LastObservation(ctx)
	if err != nil || !found {
		t.Fatalf("LastObservation: found=%v err=%v", found, err)
	}
	if !after.ObservedAt.Equal(good) {
		t.Errorf("observed_at = %s, want the last successful tick %s", after.ObservedAt, good)
	}
	if len(after.PRs) != 1 {
		t.Errorf("prs = %+v, want the previous observation intact", after.PRs)
	}

	last, err := store.LastError(ctx)
	if err != nil || last.IsNone() {
		t.Fatalf("LastError: last=%v err=%v", last, err)
	}
	tickErr := last.Unwrap()
	if !tickErr.At.Equal(bad) {
		t.Errorf("error at = %s, want %s", tickErr.At, bad)
	}
	if !strings.Contains(tickErr.Message, "exit status 1") {
		t.Errorf("error message = %q, want exit status 1", tickErr.Message)
	}

	events, err := store.Events(ctx)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want the first tick's pr_opened and one tick-error row: %+v", len(events), events)
	}
	if events[0].Kind != "pr_opened" || !events[0].At.Equal(good) {
		t.Errorf("first event = %+v, want pr_opened at the good tick", events[0])
	}
	if events[1].Kind != "tick_error" || !events[1].At.Equal(bad) {
		t.Errorf("second event = %+v, want tick_error at the bad tick", events[1])
	}
}
