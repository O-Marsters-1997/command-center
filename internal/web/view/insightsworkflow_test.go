package view_test

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/O-Marsters-1997/command-center/internal/plan/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	"github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

func TestBuildWorkflowSeries(t *testing.T) {
	t.Parallel()

	measured := func(wall time.Duration, turns int, cacheRead, input int64) agentlog.RunMetrics {
		return agentlog.RunMetrics{
			Wall: option.Some(wall), Turns: turns, CacheRead: cacheRead, Input: input,
			Tools: map[string]int{"Bash": turns},
		}
	}
	ticket := func(hash string, steps ...store.StepSpend) store.WorkflowTicketSpend {
		return store.WorkflowTicketSpend{Ticket: hash, Workflow: "wf", WorkflowHash: hash, Steps: steps}
	}

	t.Run("medians per workflow hash, unmeasured runs count toward spend only", func(t *testing.T) {
		t.Parallel()
		tickets := []store.WorkflowTicketSpend{
			ticket("abcdef0123", store.StepSpend{Step: "implement", WeightUSD: 1, Metrics: measured(time.Minute, 2, 75, 25)}),
			ticket("abcdef0123", store.StepSpend{Step: "implement", WeightUSD: 3, Metrics: measured(3*time.Minute, 4, 25, 75)}),
			ticket("abcdef0123", store.StepSpend{Step: "implement", WeightUSD: 5}),
			ticket("fedcba"),
		}
		got := view.BuildWorkflowSeries(tickets, 0.1)

		want := []view.WorkflowSeries{
			{
				Workflow: "wf", ShortHash: "abcdef0", Tickets: 3, MedianPct: 30, MedianWall: "2m0s",
				Steps: []view.StepBreakdown{{
					Step: "implement", MedianPct: 30, MedianWall: "2m0s", MedianTurns: 3, CacheReadPct: 50,
					Tools: map[string]int{"Bash": 3},
				}},
			},
			{Workflow: "wf", ShortHash: "fedcba", Tickets: 1, Steps: []view.StepBreakdown{}},
		}
		if diff := cmp.Diff(want, got, cmpopts.EquateApprox(0, 1e-9)); diff != "" {
			t.Errorf("BuildWorkflowSeries() mismatch (-want +got):\n%s", diff)
		}
	})
}
