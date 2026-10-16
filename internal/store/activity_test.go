package store_test

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

func TestStore_Activities(t *testing.T) {
	t.Parallel()
	store := openStore(t)
	seedOneTicket(t, store)
	ctx := t.Context()
	latest := testNow.Add(time.Minute)
	for _, at := range []time.Time{testNow, latest} {
		if err := store.AppendEvent(ctx, storepkg.Event{
			At: at, TicketURL: "sandbox://CC-1", Kind: storepkg.EventPushed,
		}); err != nil {
			t.Fatalf("AppendEvent: %v", err)
		}
	}
	runID := insertAgentRun(t, store)
	recordSpawn(t, store, runID)
	if err := store.RecordDisposition(ctx, storepkg.Disposition{
		RunID: plan.RunID(runID), Outcome: plan.OutcomeFailed, LastMessage: option.Some("All done."),
		EndedAt: testNow,
	}); err != nil {
		t.Fatalf("RecordDisposition: %v", err)
	}

	got, err := store.Activities(ctx)
	if err != nil {
		t.Fatalf("Activities: %v", err)
	}
	want := []storepkg.Activity{{TicketURL: "sandbox://CC-1", At: latest, LastMessage: "All done."}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Activities() mismatch (-want +got):\n%s", diff)
	}
}
