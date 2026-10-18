package config_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestWorkflows(t *testing.T) {
	t.Parallel()

	first := config.Workflows()[config.DefaultWorkflow]
	if first.Hash == "" {
		t.Fatal("Workflows()[v0].Hash is empty")
	}
	if got := config.Workflows()[config.DefaultWorkflow].Hash; got != first.Hash {
		t.Errorf("v0 hash changed between calls: %s then %s", first.Hash, got)
	}
}

func TestWorkflows_v1(t *testing.T) {
	t.Parallel()

	v1 := config.Workflows()["v1"]
	var names []plan.StepName
	for _, s := range v1.Steps {
		names = append(names, s.Name)
		if !s.Command && s.Prompt == "" {
			t.Errorf("v1 step %s has no embedded prompt", s.Name)
		}
	}
	want := []plan.StepName{"setup", "read", "implement", "check", "review", "fix", "pr"}
	if diff := cmp.Diff(want, names); diff != "" {
		t.Errorf("v1 steps mismatch (-want +got):\n%s", diff)
	}
	if v1.MaxFixRounds != 2 || v1.Hash == config.Workflows()[config.DefaultWorkflow].Hash {
		t.Errorf("v1 MaxFixRounds = %d, hash %q; want 2 and a hash of its own", v1.MaxFixRounds, v1.Hash)
	}
}
