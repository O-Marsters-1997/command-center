package plan_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/verdict"
)

func TestRulesFor(t *testing.T) {
	t.Parallel()

	obs := plan.Observation{
		Settings: map[string]plan.RepoSettings{
			"a": {
				Stacking: true, Deny: []string{".github/**"}, CompatCheck: "compat", MergifySHA: "sha256:1",
				Checks: verdict.Predicate{Success: "CI"}, VerifyCommand: []string{"just", "test"},
			},
			"b": {},
		},
		SettingsErrors: map[string]string{"c": "line 2: unknown key"},
	}

	got := plan.RulesFor(plan.Daemon{MaxAgents: 3, SpendLimit5h: 80}, obs)

	want := plan.Rules{
		Stacking:     map[string]bool{"a": true, "b": false, "c": false},
		Deny:         map[string][]string{"a": {".github/**"}, "b": nil, "c": nil},
		Checks:       map[string]verdict.Predicate{"a": {Success: "CI"}, "b": {}, "c": {}},
		MergifySHA:   map[string]string{"a": "sha256:1", "b": "", "c": ""},
		CompatCheck:  map[string]string{"a": "compat", "b": "", "c": ""},
		Verify:       map[string]bool{"a": true, "b": false, "c": false},
		MaxAgents:    3,
		SpendLimit5h: 80,
	}
	if diff := cmp.Diff(want, got, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("RulesFor(daemon, obs) mismatch (-want +got):\n%s", diff)
	}
}
