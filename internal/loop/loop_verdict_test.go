package loop_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cctest"

	storepkg "github.com/O-Marsters-1997/command-center/internal/store"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
)

func TestCheckingTicksOnlyAdvanceOnSuccessfulObserve(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"}
	if err := store.UpsertTickets(ctx, []storepkg.Ticket{ticket}); err != nil {
		t.Fatalf("UpsertTickets: %v", err)
	}

	at := testAt
	ok := newLoop(store,
		func(context.Context) (plan.Observation, error) { return plan.Observation{}, nil },
		cctest.Clock{At: at}, config.Config{}, config.Workspace{}, runner.ProcessRunner{})

	for i := range 3 {
		if err := ok.RunOnce(ctx); err != nil {
			t.Fatalf("RunOnce %d: %v", i, err)
		}
	}

	boom := errors.New("gh pr list: exit status 1")
	failing := newLoop(store, func(context.Context) (plan.Observation, error) {
		return plan.Observation{}, boom
	}, cctest.Clock{At: at.Add(time.Hour)}, config.Config{}, config.Workspace{}, runner.ProcessRunner{})
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
