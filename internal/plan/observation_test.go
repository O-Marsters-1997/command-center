package plan_test

import (
	"encoding/json"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestAnObservationSavedBeforeLocalTipsStillLoads(t *testing.T) {
	t.Parallel()

	saved := `{"observed_at":"2026-10-05T12:00:00Z",
		"prs":{"repo//cc-1":{"number":7,"head_ref":"cc-1","head_oid":"abc","base_ref":"main","base_oid":"def",
			"author_login":"a","is_draft":true,"state":2,"checks":{"CI":{"name":"CI","status":"COMPLETED",
			"conclusion":"SUCCESS","details_url":"u","started_at":"2026-10-05T11:00:00Z"}},
			"labels":["x"],"merged_at":"2026-10-05T11:30:00Z"}},
		"worktrees":{"repo//cc-1":"/tmp/wt"},"runs":{"t":{"alive":true}},
		"branch_tips":{"repo//cc-1":"abc"},"mid_merge":{},"conflicts_with_base":{}}`

	var obs plan.Observation
	if err := json.Unmarshal([]byte(saved), &obs); err != nil {
		t.Fatalf("decode: %v", err)
	}

	pr := obs.PRs["repo//cc-1"]
	if pr.State != plan.Merged || pr.Number != 7 || pr.Checks["CI"].Conclusion != "SUCCESS" || !pr.IsDraft {
		t.Errorf("pr = %+v, want the saved fields intact", pr)
	}
	if !obs.Runs["t"].Alive || obs.BranchTips["repo//cc-1"] != "abc" {
		t.Errorf("observation = %+v, want runs and branch tips intact", obs)
	}
	if obs.LocalTips != nil {
		t.Errorf("local tips = %v, want nil for a row saved before the field existed", obs.LocalTips)
	}
}
