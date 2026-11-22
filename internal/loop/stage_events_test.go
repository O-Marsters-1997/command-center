package loop_test

import (
	"context"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/adapter/runner"
	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
)

func TestRecordStageEvents(t *testing.T) {
	t.Parallel()
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "owner/cc-sandbox", Branch: "cc-1-first"}
	ctx := t.Context()
	store := openStore(t)
	if err := store.UpsertTickets(ctx, []storepkg.Ticket{ticket}); err != nil {
		t.Fatalf("UpsertTickets: %v", err)
	}
	lp := newLoop(store, func(context.Context) (plan.Observation, error) { return plan.Observation{}, nil },
		fixedClock(testAt), config.Config{}, config.Workspace{}, runner.ProcessRunner{})
	for tick := range 3 {
		if err := lp.RunOnce(ctx); err != nil {
			t.Fatalf("RunOnce tick %d: %v", tick, err)
		}
		if got := len(storetest.Events(t, store, storepkg.EventStageEntered)); got != 1 {
			t.Errorf("stage_entered events after tick %d = %d, want 1", tick, got)
		}
	}
}
