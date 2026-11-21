package loop_test

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
)

func TestAdoptHead(t *testing.T) {
	t.Parallel()

	t.Run("records the live head as the pushed tip and consumes the intent", func(t *testing.T) {
		t.Parallel()
		f := newPushFixture(t)
		dispositionAsPushed(t, f.Store, f.Ticket.URL, testAt)
		storetest.Pushed(t, f.Store, storetest.PushedOpts{
			URL: f.Ticket.URL, Tip: "cc-1-tip", Base: "main", BaseTip: "main-tip", At: testAt,
		})
		*f.Obs = draftConsumerPR(plan.Merged, nil, "forced-head")
		id, err := f.Store.QueueVerbIntent(t.Context(), storepkg.VerbRequest{
			Ticket: f.Ticket.URL, Verb: plan.VerbAdoptHead, At: testAt,
		})
		if err != nil {
			t.Fatalf("QueueVerbIntent: %v", err)
		}
		f.tick(t)

		if got := f.pushedTip(t); got != "forced-head" {
			t.Errorf("pushed tip after adopt-head = %q, want the live head", got)
		}
		consumed, err := f.Store.IntentConsumed(t.Context(), id)
		if err != nil || !consumed {
			t.Errorf("IntentConsumed(%d) = %v, %v, want true", id, consumed, err)
		}
	})
}
