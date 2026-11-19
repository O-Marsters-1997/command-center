package plan_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
)

func TestSnapshot_Overdue(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	run := func(id plan.RunID, kind plan.RunKind, age time.Duration) plan.RunSummary {
		return plan.RunSummary{
			ID: id, Kind: kind, Pgid: option.Some(int(id)),
			ProcStartedAt: option.Some(now.Add(-age)),
		}
	}
	finished := run(5, plan.RunKindCommand, time.Hour)
	finished.Outcome = option.Some(plan.OutcomeFailed)

	tests := []struct {
		name string
		runs []plan.RunSummary
		want []plan.RunID
	}{
		{
			name: "a command run past the deadline",
			runs: []plan.RunSummary{run(1, plan.RunKindCommand, plan.CommandDeadline+time.Second)},
			want: []plan.RunID{1},
		},
		{
			name: "a command run at the deadline",
			runs: []plan.RunSummary{run(1, plan.RunKindCommand, plan.CommandDeadline)},
		},
		{name: "a finished command run", runs: []plan.RunSummary{finished}},
		{
			name: "agent, follow-up and resolve runs never expire",
			runs: []plan.RunSummary{
				run(1, plan.RunKindAgent, time.Hour),
				run(2, plan.RunKindFollowUp, time.Hour),
				run(3, plan.RunKindResolve, time.Hour),
			},
		},
		{
			name: "overdue runs come back in id order",
			runs: []plan.RunSummary{
				run(7, plan.RunKindCommand, time.Hour),
				run(3, plan.RunKindCommand, time.Hour),
				run(4, plan.RunKindCommand, time.Minute),
			},
			want: []plan.RunID{3, 7},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			in := plan.Input{Now: now, Runs: map[plan.TicketURL]plan.RunSummary{}}
			for _, r := range tt.runs {
				in.Runs[plan.TicketURL(fmt.Sprintf("sandbox://%d", r.ID))] = r
			}
			if diff := cmp.Diff(tt.want, derive(plan.Rules{}, in).Overdue()); diff != "" {
				t.Errorf("Overdue() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
