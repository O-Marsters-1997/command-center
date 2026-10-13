package loop_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
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

	var settings struct {
		Permissions struct {
			Deny []string `json:"deny"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(raw, &settings); err != nil {
		t.Fatalf("settings is not valid JSON: %v\n%s", err, raw)
	}

	deny := settings.Permissions.Deny
	if !slices.ContainsFunc(deny, func(s string) bool { return strings.Contains(s, "git push") }) {
		t.Errorf("deny = %v, want an entry denying git push", deny)
	}
	if !slices.ContainsFunc(deny, func(s string) bool { return strings.Contains(s, "gh") }) {
		t.Errorf("deny = %v, want an entry denying gh", deny)
	}
	if !slices.ContainsFunc(deny, func(s string) bool { return strings.Contains(s, "WebFetch") }) {
		t.Errorf("deny = %v, want an entry denying network fetch tools", deny)
	}
}

func TestWriteAgentSystemPromptWarnsAgainstDeferringToABackgroundSubagent(t *testing.T) {
	t.Parallel()

	path := writeAgentFiles(t).SystemPromptPath

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read written system prompt: %v", err)
	}
	got := strings.ToLower(string(raw))

	for _, want := range []string{"continue the session later", "background", "subagent", "commit"} {
		if !strings.Contains(got, want) {
			t.Errorf("system prompt = %q, want it to mention %q", raw, want)
		}
	}
	if strings.Contains(got, "will not resume") {
		t.Errorf("system prompt = %q, want it no longer to say the session will not resume", raw)
	}
	if !strings.Contains(got, "do not spawn a background subagent") {
		t.Errorf("system prompt = %q, want background subagents still forbidden", raw)
	}
	if !strings.Contains(got, "foreground") {
		t.Errorf("system prompt = %q, want it to allow foreground subagents", raw)
	}
	if !strings.Contains(got, "zsh") {
		t.Errorf("system prompt = %q, want it to name the shell as zsh", raw)
	}
	if !strings.Contains(got, "rg") {
		t.Errorf("system prompt = %q, want it to prefer rg over grep --include", raw)
	}
	for _, want := range []string{"outside this repo's control", "//go:build", "exported identifier"} {
		if !strings.Contains(got, want) {
			t.Errorf("system prompt = %q, want the comment rule inlined, missing %q", raw, want)
		}
	}
}

func TestWriteAgentFilesIsIdempotent(t *testing.T) {
	t.Parallel()

	ws := writeAgentFiles(t)
	first, err := os.ReadFile(ws.SettingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := loop.WriteAgentFiles(ws); err != nil {
		t.Fatalf("second WriteAgentFiles: %v", err)
	}
	second, err := os.ReadFile(ws.SettingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Errorf("settings changed across calls:\n%s\nvs\n%s", first, second)
	}
}
