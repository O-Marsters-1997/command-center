package runner_test

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/runner"
)

func TestSpawnRejectsACommandThatBuildsAnEmptyArgv(t *testing.T) {
	t.Parallel()
	cfg := runner.SpawnConfig{AgentCommand: []string{"{session}"}, WorktreePath: t.TempDir()}

	if _, err := (runner.ProcessRunner{}).Spawn(t.Context(), cfg); err == nil {
		t.Error("Spawn(agent_command building an empty argv) = nil error, want one")
	}
}
