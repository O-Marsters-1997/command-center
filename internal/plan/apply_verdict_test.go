package plan_test

import (
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/verdict"
)

func TestRules_ApplyVerdict(t *testing.T) {
	t.Parallel()
	tk := ticket("1", "cc-1-fix")
	passed := map[string]plan.CheckState{"ci": {Status: "COMPLETED", Conclusion: "SUCCESS"}}
	rules := plan.Rules{Checks: map[string]verdict.Predicate{"r": {Success: "ci"}}}
	pushedAt := time.Unix(1000, 0)
	onMain := plan.PushRow{PushedTip: "tip", BaseBranch: "main", BaseSHAAtPush: "old", PushedAt: pushedAt}

	tests := []struct {
		name       string
		row        plan.PushRow
		head       plan.SHA
		descends   bool
		baseTip    plan.SHA
		checks     map[string]plan.CheckState
		checkedFor time.Duration
		want       verdict.Kind
	}{
		{
			name: "a main-based PR does not read base moved when main advanced",
			row:  onMain, head: "tip", baseTip: "new", checks: passed, want: verdict.ReviewMe,
		},
		{
			name: "a stacked PR still reads base moved",
			row:  plan.PushRow{PushedTip: "tip", BaseBranch: "cc-0-base", BaseSHAAtPush: "old", PushedAt: pushedAt},
			head: "tip", baseTip: "new", checks: passed, want: verdict.BaseMoved,
		},
		{
			name: "a head descending from the pushed tip has its checks counted",
			row:  onMain, head: "hand-fix", descends: true, checks: passed, want: verdict.ReviewMe,
		},
		{
			name: "a non-descendant head discards checks",
			row:  onMain, head: "forced", checks: passed, checkedFor: verdict.BoundedWait, want: verdict.NeedsYou,
		},
		{
			name: "a descendant head with no rollup is not green",
			row:  onMain, head: "hand-fix", descends: true, want: verdict.Checking,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ref := tk.Ref()
			obs := plan.NewObservation(time.Time{})
			obs.PRs[ref] = plan.PR{State: plan.Open, HeadOid: tt.head, Checks: tt.checks}
			obs.HeadDescends[ref] = tt.descends
			obs.BranchTips[plan.BranchRef{Repo: "r", Branch: tt.row.BaseBranch}] = tt.baseTip
			facts := plan.VerdictFacts{
				PushRows:    map[plan.TicketURL]plan.PushRow{tk.URL: tt.row},
				CheckingFor: map[plan.TicketURL]time.Duration{tk.URL: tt.checkedFor},
			}
			got := rules.ApplyVerdict(tk, obs, facts).Unwrap()
			if got.Kind != tt.want {
				t.Errorf("ApplyVerdict() = %v, want %v", got.Kind, tt.want)
			}
		})
	}
}
