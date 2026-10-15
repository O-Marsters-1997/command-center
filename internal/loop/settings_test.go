package loop_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
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

func TestWriteAgentSettingsDeniesPushGhAndNetworkFetch(t *testing.T) {
	t.Parallel()

	path := writeAgentFiles(t).SettingsPath

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read written settings: %v", err)
	}
	assertGolden(t, "testdata/settings.golden.json", raw)

	var settings struct {
		Permissions struct {
			Deny []string `json:"deny"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(raw, &settings); err != nil {
		t.Fatalf("settings is not valid JSON: %v\n%s", err, raw)
	}

	want := []string{"Bash(git push:*)", "Bash(gh:*)", "WebFetch", "WebSearch"}
	if !slices.Equal(settings.Permissions.Deny, want) {
		t.Errorf("deny = %v, want %v", settings.Permissions.Deny, want)
	}
}

func TestWriteAgentSystemPromptMatchesGolden(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(writeAgentFiles(t).SystemPromptPath)
	if err != nil {
		t.Fatalf("read written system prompt: %v", err)
	}
	assertGolden(t, "testdata/system-prompt.golden.txt", raw)
}

func TestWriteAgentFilesIsIdempotent(t *testing.T) {
	t.Parallel()

	ws := writeAgentFiles(t)
	first, err := os.ReadFile(ws.SettingsPath)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", ws.SettingsPath, err)
	}
	if err := loop.WriteAgentFiles(ws); err != nil {
		t.Fatalf("second WriteAgentFiles: %v", err)
	}
	second, err := os.ReadFile(ws.SettingsPath)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", ws.SettingsPath, err)
	}
	if string(first) != string(second) {
		t.Errorf("settings changed across calls:\n%s\nvs\n%s", first, second)
	}
}
