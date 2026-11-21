package plan_test

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestNewObservation(t *testing.T) {
	t.Parallel()

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
		"RepoErrors": obs.RepoErrors, "Questions": obs.Questions,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if reflect.ValueOf(value).IsNil() {
				t.Errorf("NewObservation(%v).%s = nil, want an allocated map", at, name)
			}
		})
	}
}

func TestObservation_MarkAlive(t *testing.T) {
	t.Parallel()

	obs := plan.NewObservation(time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC))
	obs.MarkAlive("ticket-1")
	if got := obs.Runs["ticket-1"].Alive; !got {
		t.Errorf("MarkAlive(ticket-1) then Runs[ticket-1].Alive = %t, want true", got)
	}
}

func TestObservation(t *testing.T) {
	t.Parallel()

	t.Run("an observation saved before local tips still decodes", func(t *testing.T) {
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
			t.Fatalf("Unmarshal(saved observation) error = %v", err)
		}
		cc1 := plan.BranchRef{Repo: "repo", Branch: "cc-1"}
		if pr := obs.PRs[cc1]; pr.State != plan.Merged || pr.Number != 7 ||
			pr.Checks["CI"].Conclusion != "SUCCESS" || !pr.IsDraft {
			t.Errorf("Unmarshal(saved).PRs[cc-1] = %+v, want the saved fields intact", pr)
		}
		if !obs.Runs["t"].Alive || obs.BranchTips[cc1] != "abc" {
			t.Errorf("Unmarshal(saved) = %+v, want runs and branch tips intact", obs)
		}
		if obs.LocalTips != nil {
			t.Errorf("Unmarshal(saved).LocalTips = %v, want nil", obs.LocalTips)
		}
	})
}

func TestObservation_Merge(t *testing.T) {
	t.Parallel()

	ref := plan.BranchRef{Repo: "r", Branch: "b"}
	dst := plan.NewObservation(time.Time{})
	dst.BranchTips[ref] = "old"
	src := plan.NewObservation(time.Time{})
	src.BranchTips[ref] = "new"
	src.Settings["r"] = plan.RepoSettings{}

	dst.Merge(src)

	if got := dst.BranchTips[ref]; got != "new" {
		t.Errorf("BranchTips after Merge = %q, want src to win", got)
	}
	if _, ok := dst.Settings["r"]; !ok {
		t.Error("Settings after Merge lack src's repo")
	}
}

func TestObservation_CarryForward(t *testing.T) {
	t.Parallel()

	mine := plan.BranchRef{Repo: "r", Branch: "b"}
	other := plan.BranchRef{Repo: "other", Branch: "b"}
	prev := plan.NewObservation(time.Time{})
	prev.BranchTips[mine], prev.BranchTips[other] = "prev-mine", "prev-other"
	prev.MergifyHash["r"], prev.MergifyHash["other"] = "h1", "h2"
	prev.Titles["t://1"] = "title"

	dst := plan.NewObservation(time.Time{})
	dst.MergifyHash["r"] = "fresh"
	dst.CarryForward(prev, "r")

	if _, ok := dst.BranchTips[other]; ok {
		t.Error("CarryForward copied another repo's tip")
	}
	if got := dst.BranchTips[mine]; got != "prev-mine" {
		t.Errorf("BranchTips[mine] = %q, want prev-mine", got)
	}
	if got := dst.MergifyHash["r"]; got != "fresh" {
		t.Errorf("MergifyHash[r] = %q, want the existing reading kept", got)
	}
	if got := dst.Titles["t://1"]; got != "title" {
		t.Errorf("Titles carried = %q, want title", got)
	}
}
