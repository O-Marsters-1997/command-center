package loop_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

func TestRecordMergedEvents(t *testing.T) {
	t.Parallel()
	mergedAt := testAt.Add(-26*time.Hour - 30*time.Minute)
	type mergedEvent struct {
		URL string
		At  time.Time
	}
	tests := []struct {
		name string
		pr   plan.PR
		want []mergedEvent
	}{
		{name: "merged PR", pr: plan.PR{Number: 41, State: plan.Merged, MergedAt: mergedAt},
			want: []mergedEvent{{URL: "sandbox://CC-1", At: mergedAt}}},
		{name: "closed unmerged PR", pr: plan.PR{Number: 41, State: plan.Closed}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			store := openStore(t)
			ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"}
			if err := store.UpsertTickets(ctx, []storepkg.Ticket{ticket}); err != nil {
				t.Fatalf("UpsertTickets: %v", err)
			}
			observed := plan.Observation{PRs: map[plan.BranchRef]plan.PR{branchRef(ticket.Repo, ticket.Branch): tt.pr}}
			lp := newLoop(store, func(context.Context) (plan.Observation, error) { return observed, nil },
				fixedClock(testAt), config.Config{}, config.Workspace{}, runner.ProcessRunner{})
			for tick := range 2 {
				if err := lp.RunOnce(ctx); err != nil {
					t.Fatalf("RunOnce tick %d: %v", tick, err)
				}
				events, err := store.Events(ctx)
				if err != nil {
					t.Fatalf("Events: %v", err)
				}
				var got []mergedEvent
				for _, event := range mergedEvents(events) {
					got = append(got, mergedEvent{URL: event.TicketURL, At: event.At})
				}
				if diff := cmp.Diff(tt.want, got); diff != "" {
					t.Errorf("merged events after tick %d mismatch (-want +got):\n%s", tick, diff)
				}
				if err := store.SaveObservation(ctx, plan.Observation{}); err != nil {
					t.Fatalf("reset observation: %v", err)
				}
			}
		})
	}
}

func mergedEvents(events []storepkg.Event) []storepkg.Event {
	var merges []storepkg.Event
	for _, e := range events {
		if e.Kind == "pr_merged" {
			merges = append(merges, e)
		}
	}
	return merges
}
