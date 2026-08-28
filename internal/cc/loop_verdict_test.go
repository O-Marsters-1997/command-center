package cc_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
)

// TestCheckingTicksOnlyAdvanceOnSuccessfulObserve is the AC's forced-failure sequence over an
// injected clock (plans/command-centre-phase-1.md § Phase 5): internal/verdict.Input.Now is
// derived from this counter, never from l.now(), so a real GitHub outage lasting hours must not
// walk a row any closer to needs_you than the successful ticks either side of it did.
func TestCheckingTicksOnlyAdvanceOnSuccessfulObserve(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	ticket := cc.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"}
	if err := store.UpsertTickets(ctx, []cc.Ticket{ticket}); err != nil {
		t.Fatalf("UpsertTickets: %v", err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	ok := cc.NewLoop(store, func(context.Context) (cc.Observation, error) { return cc.Observation{}, nil },
		fixedClock(at), cc.Config{}, cc.Workspace{}, cc.ProcessRunner{})

	for i := range 3 {
		if err := ok.RunOnce(ctx); err != nil {
			t.Fatalf("RunOnce %d: %v", i, err)
		}
	}

	// An injected clock that keeps moving, standing in for a real outage's wall-clock span,
	// while every observe across it fails.
	boom := errors.New("gh pr list: exit status 1")
	failing := cc.NewLoop(store, func(context.Context) (cc.Observation, error) {
		return cc.Observation{}, boom
	}, fixedClock(at.Add(time.Hour)), cc.Config{}, cc.Workspace{}, cc.ProcessRunner{})
	for i := range 50 {
		if err := failing.RunOnce(ctx); err == nil {
			t.Fatalf("RunOnce %d returned nil for a forced-failure observe", i)
		}
	}

	ticks, err := store.CheckingTicks(ctx)
	if err != nil {
		t.Fatalf("CheckingTicks: %v", err)
	}
	if got := ticks["sandbox://CC-1"]; got != 3 {
		t.Errorf("checking ticks = %d after 3 successful and 50 failed observes an hour apart, want 3", got)
	}

	// Observe succeeding again resumes the count from where it left off, not from the elapsed
	// wall time.
	if err := ok.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	ticks, err = store.CheckingTicks(ctx)
	if err != nil {
		t.Fatalf("CheckingTicks: %v", err)
	}
	if got := ticks["sandbox://CC-1"]; got != 4 {
		t.Errorf("checking ticks = %d after a fourth successful observe, want 4", got)
	}
}
