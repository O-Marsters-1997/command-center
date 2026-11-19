package plan_test

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestSnapshot_Reruns(t *testing.T) {
	t.Parallel()
	first := ticket("1", "cc-1")
	const lintURL = "https://github.com/o/r/actions/runs/1/job/2"
	const buildURL = "https://github.com/o/r/actions/runs/3/job/4"
	failed := func(url string, attempt int) plan.CheckState {
		return plan.CheckState{Status: "COMPLETED", Conclusion: "FAILURE", DetailsURL: url, Attempt: attempt}
	}

	tests := []struct {
		name  string
		state plan.PRState
		check map[string]plan.CheckState
		want  []plan.Rerun
	}{
		{name: "failed on attempt 1", state: plan.Open,
			check: map[string]plan.CheckState{"Lint": failed(lintURL, 1)},
			want:  []plan.Rerun{{Ticket: first.URL, DetailsURL: lintURL}}},
		{name: "failed checks list in name order", state: plan.Open,
			check: map[string]plan.CheckState{"Lint": failed(lintURL, 1), "Build": failed(buildURL, 1)},
			want: []plan.Rerun{
				{Ticket: first.URL, DetailsURL: buildURL}, {Ticket: first.URL, DetailsURL: lintURL},
			}},
		{name: "failed on attempt 2 is never rerun", state: plan.Open,
			check: map[string]plan.CheckState{"Lint": failed(lintURL, 2)}},
		{name: "attempt unread", state: plan.Open,
			check: map[string]plan.CheckState{"Lint": failed(lintURL, 0)}},
		{name: "passed on attempt 1", state: plan.Open,
			check: map[string]plan.CheckState{
				"Lint": {Status: "COMPLETED", Conclusion: "SUCCESS", DetailsURL: lintURL, Attempt: 1},
			}},
		{name: "still running", state: plan.Open,
			check: map[string]plan.CheckState{"Lint": {Status: "IN_PROGRESS", DetailsURL: lintURL, Attempt: 1}}},
		{name: "merged PR", state: plan.Merged,
			check: map[string]plan.CheckState{"Lint": failed(lintURL, 1)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			obs := plan.NewObservation(time.Time{})
			obs.PRs[key(first.Branch)] = plan.PR{Number: 1, State: tt.state, Checks: tt.check}
			snap := derive(plan.Rules{}, plan.Input{Tickets: []plan.Ticket{first}, Obs: obs})
			if diff := cmp.Diff(tt.want, snap.Reruns(), cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Reruns() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
