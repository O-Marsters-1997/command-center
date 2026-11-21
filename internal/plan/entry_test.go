package plan_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	"github.com/O-Marsters-1997/command-center/internal/plan/verdict"
)

func TestEntry_RedChecks(t *testing.T) {
	t.Parallel()

	checks := map[string]plan.CheckState{
		"lint": {DetailsURL: "https://ci/lint"},
		"test": {DetailsURL: "https://ci/test"},
	}
	failing := plan.Entry{Run: option.Some(plan.RunFact{
		Verdict: option.Some(verdict.Result{Kind: verdict.CIFailed, RedLeaves: []string{"test", "lint"}}),
	})}

	tests := []struct {
		name  string
		entry plan.Entry
		want  []plan.RedCheck
	}{
		{name: "no run", entry: plan.Entry{}},
		{name: "run without a verdict", entry: plan.Entry{Run: option.Some(plan.RunFact{})}},
		{name: "red leaves by name with their URLs", entry: failing, want: []plan.RedCheck{
			{Name: "lint", DetailsURL: "https://ci/lint"},
			{Name: "test", DetailsURL: "https://ci/test"},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if diff := cmp.Diff(tt.want, tt.entry.RedChecks(checks)); diff != "" {
				t.Errorf("RedChecks() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
