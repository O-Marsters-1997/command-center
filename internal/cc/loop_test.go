package cc_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
	"github.com/O-Marsters-1997/command-center/internal/gh"
)

func fixedClock(at time.Time) func() time.Time { return func() time.Time { return at } }

func TestRunOnceRecordsTheObservation(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

	observed := cc.Observation{
		PRs:       map[string]gh.PR{"cc-1-first": {Number: 41, HeadRef: "cc-1-first", State: gh.Open}},
		Worktrees: map[string]string{"cc-1-first": "/tmp/cc-1-first"},
	}
	loop := cc.NewLoop(store,
		func(context.Context) (cc.Observation, error) { return observed, nil },
		fixedClock(at), cc.Config{}, cc.Workspace{}, cc.ProcessRunner{})
	if err := loop.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	got, ok, err := store.LastObservation(ctx)
	if err != nil || !ok {
		t.Fatalf("LastObservation: ok=%v err=%v", ok, err)
	}
	if !got.ObservedAt.Equal(at) {
		t.Errorf("observed_at = %s, want the injected clock %s", got.ObservedAt, at)
	}
	if got.PRs["cc-1-first"].State != gh.Open {
		t.Errorf("prs = %+v", got.PRs)
	}
}

func TestRunOnceAppliesQueuedLaunchIntents(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	ticket := cc.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"}
	if err := store.UpsertTickets(ctx, []cc.Ticket{ticket}); err != nil {
		t.Fatalf("UpsertTickets: %v", err)
	}
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	if err := store.QueueLaunchIntent(ctx, "sandbox://CC-1", "hash-1", "group-a", at); err != nil {
		t.Fatalf("QueueLaunchIntent: %v", err)
	}

	stub := func(context.Context) (cc.Observation, error) { return cc.Observation{}, nil }
	loop := cc.NewLoop(store, stub, fixedClock(at), cc.Config{}, cc.Workspace{}, cc.ProcessRunner{})
	if err := loop.RunOnce(ctx); err != nil {
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
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	good := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	bad := good.Add(15 * time.Second)

	ticket := cc.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"}
	if err := store.UpsertTickets(ctx, []cc.Ticket{ticket}); err != nil {
		t.Fatalf("UpsertTickets: %v", err)
	}

	observed := cc.Observation{PRs: map[string]gh.PR{"cc-1-first": {Number: 41, State: gh.Open}}}
	ok := cc.NewLoop(store,
		func(context.Context) (cc.Observation, error) { return observed, nil },
		fixedClock(good), cc.Config{}, cc.Workspace{}, cc.ProcessRunner{})
	if err := ok.RunOnce(ctx); err != nil {
		t.Fatalf("first RunOnce: %v", err)
	}

	// Queued after the only successful tick, so the failing tick below is the only one that
	// could apply it — and inv. 10 says a failed observe must apply no transition at all.
	if err := store.QueueLaunchIntent(ctx, "sandbox://CC-1", "hash-1", "group-a", bad); err != nil {
		t.Fatalf("QueueLaunchIntent: %v", err)
	}

	boom := errors.New("gh pr list: exit status 1")
	failing := cc.NewLoop(store, func(context.Context) (cc.Observation, error) {
		return cc.Observation{}, boom
	}, fixedClock(bad), cc.Config{}, cc.Workspace{}, cc.ProcessRunner{})

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

	// inv. 10: the tick applied no transition, so the last good observation is still the one
	// the page renders — and its age keeps growing.
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

	tickErr, found, err := store.LastError(ctx)
	if err != nil || !found {
		t.Fatalf("LastError: found=%v err=%v", found, err)
	}
	if !tickErr.At.Equal(bad) {
		t.Errorf("error at = %s, want %s", tickErr.At, bad)
	}
	if !strings.Contains(tickErr.Message, "exit status 1") {
		t.Errorf("error message = %q", tickErr.Message)
	}

	events, err := store.Events(ctx)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want exactly one tick-error row: %+v", len(events), events)
	}
	if events[0].Kind != "tick_error" || !events[0].At.Equal(bad) {
		t.Errorf("event = %+v", events[0])
	}
}
