package insight_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/insight"
)

func TestParkReason(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, detail, want string }{
		{"verify log path", "verify_command failed; log at /data/logs/12.log", "verify_command failed"},
		{"branch name", "issue-12/foo no longer merges cleanly into main", "… no longer merges cleanly into main"},
		{"workflow hash", "workflow ab12cd is not in this build", "workflow … is not in this build"},
		{"plain text", "waiting on review", "waiting on review"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := insight.ParkReason(tt.detail); got != tt.want {
				t.Errorf("ParkReason(%q) = %q, want %q", tt.detail, got, tt.want)
			}
		})
	}
}

func TestParkLedger(t *testing.T) {
	t.Parallel()
	parks := []insight.ParkFact{
		{Reason: "conflict", Detail: "a conflicts", Tokens: 10},
		{Reason: "verify", Detail: "verify 1", Tokens: 500},
		{Reason: "conflict", Detail: "b conflicts", Tokens: 5},
		{Reason: "alpha", Detail: "alpha 1", Tokens: 500},
	}
	want := []insight.ParkRow{
		{Reason: "conflict", Example: "a conflicts", Times: 2, Tokens: 15},
		{Reason: "alpha", Example: "alpha 1", Times: 1, Tokens: 500},
		{Reason: "verify", Example: "verify 1", Times: 1, Tokens: 500},
	}
	if diff := cmp.Diff(want, insight.ParkLedger(parks)); diff != "" {
		t.Errorf("ParkLedger() mismatch (-want +got):\n%s", diff)
	}
	if got := insight.ParkLedger(nil); len(got) != 0 {
		t.Errorf("ParkLedger(nil) = %v, want none", got)
	}
}

func TestJobLedger(t *testing.T) {
	t.Parallel()
	failed := func(ticket, repo, name string) insight.FailedCheck {
		return insight.FailedCheck{Ticket: ticket, Repo: repo, Name: name}
	}
	tests := []struct {
		name    string
		facts   insight.ReworkFacts
		rollups map[string][]string
		want    []insight.JobRow
	}{
		{
			name: "groups and ranks by failures then tokens",
			facts: insight.ReworkFacts{
				Failed: []insight.FailedCheck{
					failed("t1", "r", "unit"), failed("t2", "r", "unit"), failed("t1", "r", "lint"),
					failed("t3", "other", "unit"),
				},
				Fixes: []insight.FixRun{{Ticket: "t1", Tokens: 100, Checks: []string{"lint"}}},
			},
			want: []insight.JobRow{
				{Name: "unit", Repo: "r", Failures: 2},
				{Name: "lint", Repo: "r", Failures: 1, FixTokens: 100},
				{Name: "unit", Repo: "other", Failures: 1},
			},
		},
		{
			name: "default rollup glob drops aggregate checks",
			facts: insight.ReworkFacts{
				Failed: []insight.FailedCheck{failed("t1", "r", "ci-status-check"), failed("t1", "r", "unit")},
			},
			rollups: map[string][]string{"r": plan.DefaultRollupChecks},
			want:    []insight.JobRow{{Name: "unit", Repo: "r", Failures: 1}},
		},
		{
			name: "repo rollups replace the default",
			facts: insight.ReworkFacts{
				Failed: []insight.FailedCheck{
					failed("t1", "r", "ci-status-check"), failed("t1", "r", "gate"), failed("t1", "r", "unit"),
				},
			},
			rollups: map[string][]string{"r": {"g*"}},
			want: []insight.JobRow{
				{Name: "ci-status-check", Repo: "r", Failures: 1},
				{Name: "unit", Repo: "r", Failures: 1},
			},
		},
		{
			name:    "fix tokens split evenly over non-rollup checks and sum to the run",
			rollups: map[string][]string{"r": plan.DefaultRollupChecks},
			facts: insight.ReworkFacts{
				Failed: []insight.FailedCheck{failed("t1", "r", "a")},
				Fixes: []insight.FixRun{
					{Ticket: "t1", Tokens: 100, Checks: []string{"c", "a", "b", "x-status-check"}},
					{Ticket: "t1", Tokens: 7, Checks: []string{"x-status-check"}},
				},
			},
			want: []insight.JobRow{
				{Name: "a", Repo: "r", Failures: 1, FixTokens: 34},
				{Name: "b", Repo: "r", FixTokens: 33},
				{Name: "c", Repo: "r", FixTokens: 33},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := insight.JobLedger(tt.facts, tt.rollups)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("JobLedger() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
