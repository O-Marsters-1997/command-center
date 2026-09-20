package cc_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/cc"
)

func TestWriteAgentSettingsDeniesPushGhAndNetworkFetch(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "agent.json")
	if err := cc.WriteAgentSettings(path); err != nil {
		t.Fatalf("WriteAgentSettings: %v", err)
	}

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

	path := filepath.Join(t.TempDir(), "system-prompt.md")
	if err := cc.WriteAgentSystemPrompt(path); err != nil {
		t.Fatalf("WriteAgentSystemPrompt: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read written system prompt: %v", err)
	}
	got := strings.ToLower(string(raw))

	for _, want := range []string{"single-shot", "background", "subagent", "commit"} {
		if !strings.Contains(got, want) {
			t.Errorf("system prompt = %q, want it to mention %q", raw, want)
		}
	}
}

func TestWriteAgentSettingsIsIdempotent(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "agent.json")
	if err := cc.WriteAgentSettings(path); err != nil {
		t.Fatalf("first WriteAgentSettings: %v", err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := cc.WriteAgentSettings(path); err != nil {
		t.Fatalf("second WriteAgentSettings: %v", err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Errorf("settings changed across calls:\n%s\nvs\n%s", first, second)
	}
}
