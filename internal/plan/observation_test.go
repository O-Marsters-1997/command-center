package plan_test

import (
	"encoding/json"
	"maps"
	"reflect"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestNewObservationAllocatesEveryMapAndMarksAlive(t *testing.T) {
	at := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	obs := plan.NewObservation(at)
	if obs.ObservedAt != at {
		t.Errorf("NewObservation(%v).ObservedAt = %v, want %v", at, obs.ObservedAt, at)
	}
	for name, value := range map[string]any{
		"PRs": obs.PRs, "Worktrees": obs.Worktrees, "Runs": obs.Runs,
		"MergifyHash": obs.MergifyHash, "BranchTips": obs.BranchTips, "LocalTips": obs.LocalTips,
		"Titles": obs.Titles, "MidMerge": obs.MidMerge, "ConflictsWithBase": obs.ConflictsWithBase,
		"ConflictsWithPeer": obs.ConflictsWithPeer, "Settings": obs.Settings,
		"SettingsErrors": obs.SettingsErrors, "SettingsSources": obs.SettingsSources,
		"RepoErrors": obs.RepoErrors,
	} {
		if reflect.ValueOf(value).IsNil() {
			t.Errorf("NewObservation(%v).%s = nil, want allocated map", at, name)
		}
	}
	obs.MarkAlive("ticket-1")
	if !obs.Runs["ticket-1"].Alive {
		t.Error("MarkAlive(ticket-1) did not mark the run alive")
	}
}

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

	cc1 := plan.BranchRef{Repo: "repo", Branch: "cc-1"}
	pr := obs.PRs[cc1]
	if pr.State != plan.Merged || pr.Number != 7 || pr.Checks["CI"].Conclusion != "SUCCESS" || !pr.IsDraft {
		t.Errorf("pr = %+v, want the saved fields intact", pr)
	}
	if !obs.Runs["t"].Alive || obs.BranchTips[cc1] != "abc" {
		t.Errorf("observation = %+v, want runs and branch tips intact", obs)
	}
	if obs.LocalTips != nil {
		t.Errorf("local tips = %v, want nil for a row saved before the field existed", obs.LocalTips)
	}
}

func TestBranchRefKeysRoundTripAsRepoSlashSlashBranch(t *testing.T) {
	t.Parallel()

	want := map[plan.BranchRef]bool{{Repo: "acme/web", Branch: "cc-1/part"}: true}
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if got, wantJSON := string(raw), `{"acme/web//cc-1/part":true}`; got != wantJSON {
		t.Errorf("encoded = %s, want %s", got, wantJSON)
	}

	var got map[plan.BranchRef]bool
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !maps.Equal(want, got) {
		t.Errorf("round trip = %v, want %v", got, want)
	}
}

func TestBranchRefRejectsAKeyWithoutTheSeparator(t *testing.T) {
	t.Parallel()

	var got map[plan.BranchRef]bool
	if err := json.Unmarshal([]byte(`{"no-separator":true}`), &got); err == nil {
		t.Errorf("decode = %v, want an error", got)
	}
}
