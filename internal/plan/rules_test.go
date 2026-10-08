package plan_test

import (
	"slices"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/verdict"
)

func TestRulesForIndexesEachObservedRepoByName(t *testing.T) {
	t.Parallel()

	obs := plan.Observation{
		Settings: map[string]plan.RepoSettings{
			"a": {
				Stacking: true, Deny: []string{".github/**"}, CompatCheck: "compat", MergifySHA: "sha256:1",
				Checks: verdict.Predicate{Success: "CI"},
			},
			"b": {},
		},
		SettingsErrors: map[string]string{"c": "line 2: unknown key"},
	}

	rules := plan.RulesFor(plan.Daemon{MaxAgents: 3, SpendLimit5h: 80}, obs)

	if rules.MaxAgents != 3 || rules.SpendLimit5h != 80 {
		t.Errorf("max_agents/spend_limit_5h = %d/%d, want 3/80", rules.MaxAgents, rules.SpendLimit5h)
	}
	if !rules.Stacking["a"] || rules.Stacking["b"] {
		t.Errorf("stacking = %v, want a only", rules.Stacking)
	}
	if !slices.Equal(rules.Deny["a"], []string{".github/**"}) || len(rules.Deny["b"]) != 0 {
		t.Errorf("deny = %v", rules.Deny)
	}
	if rules.Checks["a"].Success != "CI" || !rules.Checks["b"].IsZero() {
		t.Errorf("checks = %v", rules.Checks)
	}
	if rules.MergifySHA["a"] != "sha256:1" || rules.CompatCheck["a"] != "compat" {
		t.Errorf("mergify/compat = %v/%v", rules.MergifySHA, rules.CompatCheck)
	}
	for _, repo := range []string{"b", "c"} {
		if _, ok := rules.Stacking[repo]; !ok {
			t.Errorf("repo %s is missing from the rules, so scope checks would not see it", repo)
		}
	}
}
