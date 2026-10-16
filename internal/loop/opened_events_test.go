package loop_test

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/adapter/runner"
	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

func TestRecordOpenedEvents(t *testing.T) {
	t.Parallel()
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "owner/cc-sandbox", Branch: "cc-1-first"}
	prObserved := plan.Observation{PRs: map[plan.BranchRef]plan.PR{
		branchRef(ticket.Repo, ticket.Branch): {Number: 41, State: plan.Open},
	}}
	type openedEvent struct{ URL, Detail string }
	tests := []struct {
		name    string
		initial plan.Observation
		want    []openedEvent
	}{
		{name: "PR appears", initial: plan.Observation{}, want: []openedEvent{{
			URL: ticket.URL, Detail: "PR #41 https://github.com/owner/cc-sandbox/pull/41",
		}}},
		{name: "PR already in previous observation", initial: prObserved},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			store := openStore(t)
			if err := store.UpsertTickets(ctx, []storepkg.Ticket{ticket}); err != nil {
				t.Fatalf("UpsertTickets: %v", err)
			}
			if err := store.SaveObservation(ctx, tt.initial); err != nil {
				t.Fatalf("SaveObservation: %v", err)
			}
			observed := tt.initial
			lp := newLoop(store, func(context.Context) (plan.Observation, error) { return observed, nil },
				fixedClock(testAt), config.Config{}, config.Workspace{}, runner.ProcessRunner{})
			for tick := range 3 {
				if tick == 1 {
					observed = prObserved
				}
				if err := lp.RunOnce(ctx); err != nil {
					t.Fatalf("RunOnce tick %d: %v", tick, err)
				}
				events, err := store.Events(ctx)
				if err != nil {
					t.Fatalf("Events: %v", err)
				}
				var got []openedEvent
				for _, event := range openedEvents(events) {
					got = append(got, openedEvent{URL: event.TicketURL, Detail: event.Detail})
				}
				var want []openedEvent
				if tick > 0 {
					want = tt.want
				}
				if diff := cmp.Diff(want, got); diff != "" {
					t.Errorf("opened events after tick %d mismatch (-want +got):\n%s", tick, diff)
				}
			}
		})
	}
}

func openedEvents(events []storepkg.Event) []storepkg.Event {
	var opened []storepkg.Event
	for _, e := range events {
		if e.Kind == "pr_opened" {
			opened = append(opened, e)
		}
	}
	return opened
}
