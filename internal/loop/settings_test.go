package loop_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/loop"
)

func writeAgentFiles(t *testing.T) config.Workspace {
	t.Helper()

	dir := t.TempDir()
	ws := config.Workspace{
		SettingsPath:     filepath.Join(dir, "agent.json"),
		SystemPromptPath: filepath.Join(dir, "system-prompt.md"),
	}
	if err := loop.WriteAgentFiles(ws); err != nil {
		t.Fatalf("WriteAgentFiles: %v", err)
	}
	return ws
}

func TestWriteAgentSettingsMatchesGolden(t *testing.T) {
	t.Parallel()

	path := writeAgentFiles(t).SettingsPath

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read written settings: %v", err)
	}
	assertGolden(t, "testdata/settings.golden.json", raw)
}

func TestWriteAgentSystemPromptMatchesGolden(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(writeAgentFiles(t).SystemPromptPath)
	if err != nil {
		t.Fatalf("read written system prompt: %v", err)
	}
	assertGolden(t, "testdata/system-prompt.golden.txt", raw)
}
