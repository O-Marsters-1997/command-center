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
	if !strings.Contains(got, "do not spawn a background subagent") {
		t.Errorf("system prompt = %q, want background subagents still forbidden", raw)
	}
	if !strings.Contains(got, "foreground") {
		t.Errorf("system prompt = %q, want it to allow foreground subagents", raw)
	}
	if !strings.Contains(got, "digest subagent") {
		t.Errorf("system prompt = %q, want it to delegate read-heavy work to the digest subagent", raw)
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

func TestWriteAgentDigestDefinitionDefinesDigestOnHaikuWithReadOnlyTools(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "agents.json")
	if err := cc.WriteAgentDigestDefinition(path); err != nil {
		t.Fatalf("WriteAgentDigestDefinition: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read written agents definition: %v", err)
	}

	var defs map[string]struct {
		Prompt string   `json:"prompt"`
		Model  string   `json:"model"`
		Tools  []string `json:"tools"`
	}
	if err := json.Unmarshal(raw, &defs); err != nil {
		t.Fatalf("agents definition is not valid JSON: %v\n%s", err, raw)
	}

	digest, ok := defs["digest"]
	if !ok {
		t.Fatalf("agents definition = %v, want a %q entry", defs, "digest")
	}
	if digest.Model != "haiku" {
		t.Errorf("digest model = %q, want haiku", digest.Model)
	}
	for _, want := range []string{"Read", "Grep", "Glob", "Bash"} {
		if !slices.Contains(digest.Tools, want) {
			t.Errorf("digest tools = %v, want %q", digest.Tools, want)
		}
	}
	if !strings.Contains(digest.Prompt, "codegraph explore") {
		t.Errorf("digest prompt = %q, want it to query an indexed repo through codegraph explore", digest.Prompt)
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
