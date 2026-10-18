package config_test

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/config"
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
