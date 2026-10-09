package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/config"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "command-centre.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestLoadConfigIgnoresATaskBlock covers phase 7: [[task]] is no longer decoded into anything, so
// a config still carrying one from before the tracker import lands loads exactly as if it were
// absent, rather than refusing to start.
func TestLoadConfigIgnoresATaskBlock(t *testing.T) {
	t.Parallel()

	body := `
max_agents = 2
port       = 8080

[[task]]
ticket_url = "sandbox://CC-1"
repo       = "cc-sandbox"
branch     = "cc-1-first"
blocked_by = []

[[repo]]
name = "cc-sandbox"
path = "cc-sandbox"
`
	got, err := config.LoadConfig(writeConfig(t, body))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got.MaxAgents != 2 || got.Port != 8080 {
		t.Errorf("max_agents/port = %d/%d, want 2/8080", got.MaxAgents, got.Port)
	}
	if len(got.LegacyRepos) != 1 || got.LegacyRepos[0].Name != "cc-sandbox" {
		t.Errorf("legacy repos = %+v", got.LegacyRepos)
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	t.Parallel()

	body := "[[repo]]\nname = \"r\"\npath = \"r\"\n"
	got, err := config.LoadConfig(writeConfig(t, body))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got.Port != 7777 {
		t.Errorf("port = %d, want default 7777", got.Port)
	}
	if got.MaxAgents != 1 {
		t.Errorf("max_agents = %d, want default 1", got.MaxAgents)
	}
	want := []string{
		"claude", "-p", "{prompt}",
		"--output-format", "stream-json", "--verbose",
		"--settings", "{settings}",
		"--agents", "{agents}",
		"--append-system-prompt-file", "{system_prompt}",
		"--permission-mode", "auto",
		"--model", "{model}",
	}
	if !slices.Equal(got.AgentCommand, want) {
		t.Errorf("agent_command = %q, want default %q", got.AgentCommand, want)
	}
}

func TestLoadConfigSpendLimit5h(t *testing.T) {
	t.Parallel()

	body := "spend_limit_5h = 80\n\n[[repo]]\nname = \"r\"\npath = \"r\"\n"
	got, err := config.LoadConfig(writeConfig(t, body))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got.SpendLimit5h != 80 {
		t.Errorf("spend_limit_5h = %d, want 80", got.SpendLimit5h)
	}
}

func TestLoadConfigSpendLimit5hDefaultsToUnset(t *testing.T) {
	t.Parallel()

	body := "[[repo]]\nname = \"r\"\npath = \"r\"\n"
	got, err := config.LoadConfig(writeConfig(t, body))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got.SpendLimit5h != 0 {
		t.Errorf("spend_limit_5h = %d, want default 0 (unset, never pauses)", got.SpendLimit5h)
	}
}

func withRequiredParts(head ...string) []string {
	return append(slices.Clone(head),
		"--agents", "{agents}",
		"--append-system-prompt-file", "{system_prompt}",
		"--permission-mode", "auto",
		"--model", "{model}",
	)
}

func agentCommandLine(t *testing.T, argv []string) string {
	t.Helper()
	quoted, err := json.Marshal(argv)
	if err != nil {
		t.Fatal(err)
	}
	return "agent_command = " + string(quoted) + "\n"
}

func TestLoadConfigAgentCommandOverridesTheDefault(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		line string
		want []string
	}{
		{
			name: "a config naming its own argv replaces the default outright, never appending to it",
			line: agentCommandLine(t, withRequiredParts("my-agent", "--model", "claude-opus-5")),
			want: withRequiredParts("my-agent", "--model", "claude-opus-5"),
		},
		{
			// An empty array is how an operator turns spawning off: Spawn rejects it by design.
			name: "an explicit empty array stays empty rather than falling back",
			line: "agent_command = []\n",
			want: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := config.LoadConfig(writeConfig(t, tt.line))
			if err != nil {
				t.Fatalf("LoadConfig: %v", err)
			}
			if !slices.Equal(got.AgentCommand, tt.want) {
				t.Errorf("agent_command = %q, want %q", got.AgentCommand, tt.want)
			}
		})
	}
}

func TestLoadConfigRefusesAnArgvMissingARequiredPart(t *testing.T) {
	complete := []string{
		"claude", "-p", "{prompt}",
		"--agents", "{agents}",
		"--append-system-prompt-file", "{system_prompt}",
		"--permission-mode", "auto",
		"--model", "{model}",
	}
	without := func(drop ...string) []string {
		var argv []string
		for _, a := range complete {
			if !slices.Contains(drop, a) {
				argv = append(argv, a)
			}
		}
		return argv
	}

	tests := []struct {
		name    string
		argv    []string
		missing string
	}{
		{"no permission mode", without("--permission-mode"), "--permission-mode"},
		{"no agents placeholder", without("{agents}"), "{agents}"},
		{"no system prompt placeholder", without("{system_prompt}"), "{system_prompt}"},
		{"no model placeholder", without("{model}"), "{model}"},
	}
	for _, tt := range tests {
		t.Run(tt.name+" in agent_command", func(t *testing.T) {
			t.Setenv("CC_DATA_DIR", t.TempDir())

			_, err := config.LoadConfig(writeConfig(t, agentCommandLine(t, tt.argv)))
			if err == nil || !strings.Contains(err.Error(), tt.missing) {
				t.Errorf("LoadConfig error = %v, want one naming %s", err, tt.missing)
			}
		})
		t.Run(tt.name+" in CC_AGENT_COMMAND", func(t *testing.T) {
			t.Setenv("CC_DATA_DIR", t.TempDir())
			env, err := json.Marshal(tt.argv)
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("CC_AGENT_COMMAND", string(env))

			_, err = config.LoadConfig(writeConfig(t, agentCommandLine(t, complete)))
			if err == nil || !strings.Contains(err.Error(), tt.missing) {
				t.Errorf("LoadConfig error = %v, want one naming %s", err, tt.missing)
			}
		})
	}
}

func TestLoadConfigMaxTurns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "unset leaves agent_command untouched",
			body: "[[repo]]\nname = \"r\"\npath = \"r\"\n",
			want: []string{
				"claude", "-p", "{prompt}",
				"--output-format", "stream-json", "--verbose",
				"--settings", "{settings}",
				"--agents", "{agents}",
				"--append-system-prompt-file", "{system_prompt}",
				"--permission-mode", "auto",
				"--model", "{model}",
			},
		},
		{
			name: "set appends --max-turns with the configured value",
			body: "max_turns = 40\n\n[[repo]]\nname = \"r\"\npath = \"r\"\n",
			want: []string{
				"claude", "-p", "{prompt}",
				"--output-format", "stream-json", "--verbose",
				"--settings", "{settings}",
				"--agents", "{agents}",
				"--append-system-prompt-file", "{system_prompt}",
				"--permission-mode", "auto",
				"--model", "{model}",
				"--max-turns", "40",
			},
		},
		{
			name: "an explicit empty agent_command stays empty so spawning stays off",
			body: "max_turns = 40\nagent_command = []\n\n[[repo]]\nname = \"r\"\npath = \"r\"\n",
			want: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := config.LoadConfig(writeConfig(t, tt.body))
			if err != nil {
				t.Fatalf("LoadConfig: %v", err)
			}
			if !slices.Equal(got.AgentCommand, tt.want) {
				t.Errorf("agent_command = %q, want %q", got.AgentCommand, tt.want)
			}
		})
	}
}

func TestLoadConfigReviewMaxTurns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		body    string
		want    string
		wantErr string
	}{
		{name: "unset defaults without an implement cap", body: "", want: "20"},
		{name: "unset halves a low implement cap", body: "max_turns = 10\n", want: "5"},
		{name: "unset keeps the default under a high implement cap", body: "max_turns = 100\n", want: "20"},
		{name: "explicit value below the implement cap", body: "max_turns = 60\nreview_max_turns = 15\n", want: "15"},
		{
			name: "explicit value not below the implement cap",
			body: "max_turns = 20\nreview_max_turns = 20\n", wantErr: "must be below",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := config.LoadConfig(writeConfig(t, tt.body+"\n[[repo]]\nname = \"r\"\npath = \"r\"\n"))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("LoadConfig error = %v, want one containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadConfig: %v", err)
			}
			n := len(got.ReviewAgentCommand)
			if n < 2 || got.ReviewAgentCommand[n-2] != "--max-turns" || got.ReviewAgentCommand[n-1] != tt.want {
				t.Errorf("review command tail = %q, want --max-turns %s", got.ReviewAgentCommand, tt.want)
			}
		})
	}
}

// TestAgentCommandEnvOverridesTheTrackedOne covers phase 5: the config is tracked and the same
// on every machine, so a local wrapper (caffeinate, a sandbox) arrives by environment.
func TestAgentCommandEnvOverridesTheTrackedOne(t *testing.T) {
	t.Setenv("CC_DATA_DIR", t.TempDir())
	tracked := withRequiredParts("claude", "-p", "{prompt}")
	path := writeConfig(t, agentCommandLine(t, tracked))

	got, err := config.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.AgentCommand, tracked) {
		t.Errorf("agent_command with no override = %v, want the tracked %v", got.AgentCommand, tracked)
	}

	override := withRequiredParts("caffeinate", "-i", "claude", "-p", "{prompt}")
	env, err := json.Marshal(override)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CC_AGENT_COMMAND", string(env))
	got, err = config.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := override; !slices.Equal(got.AgentCommand, want) {
		t.Errorf("agent_command = %v, want the override %v", got.AgentCommand, want)
	}

	for _, bad := range []string{"caffeinate -i claude", "[]", `{"a":1}`} {
		t.Setenv("CC_AGENT_COMMAND", bad)
		if _, err := config.LoadConfig(path); err == nil {
			t.Errorf("CC_AGENT_COMMAND=%q was accepted, want a refusal", bad)
		}
	}
}

func TestLoadConfigBoardPollSeconds(t *testing.T) {
	t.Parallel()

	got, err := config.LoadConfig(writeConfig(t, "[[repo]]\nname = \"r\"\npath = \"r\"\n"))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got.BoardPollSeconds != 5 {
		t.Errorf("board_poll_seconds = %d, want default 5", got.BoardPollSeconds)
	}

	got, err = config.LoadConfig(writeConfig(t, "board_poll_seconds = 1\n[[repo]]\nname = \"r\"\npath = \"r\"\n"))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got.BoardPollSeconds != 1 {
		t.Errorf("board_poll_seconds = %d, want 1", got.BoardPollSeconds)
	}
}

func TestLoadConfigRejectsANonPositiveBoardPoll(t *testing.T) {
	t.Parallel()

	_, err := config.LoadConfig(writeConfig(t, "board_poll_seconds = 0\n[[repo]]\nname = \"r\"\npath = \"r\"\n"))
	if err == nil {
		t.Fatal("LoadConfig accepted board_poll_seconds = 0")
	}
}

func TestLoadConfigRejectsAPerRepoKeyAndPointsToTheSettingsFile(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ key, line string }{
		{"tracker", `tracker = "github"`},
		{"stacking", "stacking = true"},
		{"deny", `deny = ["go.mod"]`},
		{"compat_check", `compat_check = "x"`},
		{"mergify_sha", `mergify_sha = "sha256:1"`},
		{"verify_command", `verify_command = ["true"]`},
		{"checks", "[repo.checks]\nsuccess = \"CI\""},
	} {
		t.Run(tt.key, func(t *testing.T) {
			t.Parallel()

			_, err := config.LoadConfig(writeConfig(t, "[[repo]]\nname = \"r\"\npath = \"r\"\n"+tt.line+"\n"))
			if err == nil {
				t.Fatalf("LoadConfig accepted %q in a [[repo]] block", tt.key)
			}
			if !strings.Contains(err.Error(), `"`+tt.key+`"`) || !strings.Contains(err.Error(), config.SettingsFile) {
				t.Errorf("err = %v, want it to name %q and %s", err, tt.key, config.SettingsFile)
			}
		})
	}
}

func TestLoadConfigIgnoresTheRemovedGeneratedKeys(t *testing.T) {
	t.Parallel()

	body := `
[[repo]]
name          = "cc-sandbox"
path          = "cc-sandbox"
generated     = ["dist/**"]
build_command = ["just", "assets"]
`
	got, err := config.LoadConfig(writeConfig(t, body))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(got.LegacyRepos) != 1 || got.LegacyRepos[0].Name != "cc-sandbox" {
		t.Errorf("legacy repos = %+v", got.LegacyRepos)
	}
}

func TestCheckedInConfigPinsTheDefaultModel(t *testing.T) {
	t.Parallel()

	shipped, err := config.LoadConfig("../../cc/config.toml")
	if err != nil {
		t.Fatalf("LoadConfig(cc/config.toml): %v", err)
	}
	defaults, err := config.LoadConfig(writeConfig(t, "[[repo]]\nname = \"r\"\npath = \"r\"\n"))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !slices.Equal(shipped.AgentCommand, defaults.AgentCommand) {
		t.Errorf("cc/config.toml agent_command = %q, want the default %q", shipped.AgentCommand, defaults.AgentCommand)
	}
}
