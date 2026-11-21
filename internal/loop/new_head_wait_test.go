package loop_test

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
)

func TestNewHeadRestartsTheWait(t *testing.T) {
	t.Parallel()
	f := newPushFixture(t)
	dispositionAsPushed(t, f.Store, f.Ticket.URL, testAt)
	storetest.Pushed(t, f.Store, storetest.PushedOpts{
		URL: f.Ticket.URL, Tip: "cc-1-tip", Base: "main", BaseTip: "main-tip", At: testAt,
	})
	ref := branchRef("repo", "cc-1")
	ticks := func() int {
		t.Helper()
		got, err := f.Store.CheckingTicks(t.Context())
		if err != nil {
			t.Fatalf("CheckingTicks: %v", err)
		}
		return got[plan.TicketURL(f.Ticket.URL)]
	}
	observeHead := func(head string) {
		*f.Obs = draftConsumerPR(plan.Merged, nil, head)
		f.Obs.HeadDescends = map[plan.BranchRef]bool{ref: true}
		f.tick(t)
	}

	observeHead("hand-fix-1")
	observeHead("hand-fix-1")
	if got := ticks(); got != 1 {
		t.Fatalf("checking ticks after a head held for two observations = %d, want 1", got)
	}
	observeHead("hand-fix-2")
	if got := ticks(); got != 0 {
		t.Errorf("checking ticks after a new descendant head = %d, want 0", got)
	}
}
