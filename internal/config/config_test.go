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

var testEnv = config.Env{Home: "/home/test", UserConfigDir: "/home/test/.config"}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "command-centre.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestLoadIgnoresATaskBlock covers phase 7: [[task]] is no longer decoded into anything, so
// a config still carrying one from before the tracker import lands loads exactly as if it were
// absent, rather than refusing to start.
func TestLoadIgnoresATaskBlock(t *testing.T) {
	t.Parallel()

	body := `
max_agents = 2
port       = 8080

[[task]]
ticket_url = "sandbox://CC-1"
repo       = "cc-sandbox"
branch     = "cc-1-first"
blocked_by = []
`
	got, err := config.Load(writeConfig(t, body), testEnv)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.MaxAgents != 2 || got.Port != 8080 {
		t.Errorf("max_agents/port = %d/%d, want 2/8080", got.MaxAgents, got.Port)
	}
}

func TestLoadDefaults(t *testing.T) {
	t.Parallel()

	got, err := config.Load(writeConfig(t, ""), testEnv)
	if err != nil {
		t.Fatalf("Load: %v", err)
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
		"--append-system-prompt-file", "{system_prompt}",
		"--permission-mode", "auto",
		"--model", "{model}",
		"{session}",
	}
	if !slices.Equal(got.AgentCommand, want) {
		t.Errorf("agent_command = %q, want default %q", got.AgentCommand, want)
	}
}

func TestLoadSpendLimit5h(t *testing.T) {
	t.Parallel()

	got, err := config.Load(writeConfig(t, "spend_limit_5h = 80\n"), testEnv)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.SpendLimit5h != 80 {
		t.Errorf("spend_limit_5h = %d, want 80", got.SpendLimit5h)
	}
}

func TestLoadSpendLimit5hDefaultsToUnset(t *testing.T) {
	t.Parallel()

	got, err := config.Load(writeConfig(t, ""), testEnv)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.SpendLimit5h != 0 {
		t.Errorf("spend_limit_5h = %d, want default 0 (unset, never pauses)", got.SpendLimit5h)
	}
}

func withRequiredParts(head ...string) []string {
	return append(slices.Clone(head),
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

func TestLoadAgentCommandOverridesTheDefault(t *testing.T) {
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

			got, err := config.Load(writeConfig(t, tt.line), testEnv)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if !slices.Equal(got.AgentCommand, tt.want) {
				t.Errorf("agent_command = %q, want %q", got.AgentCommand, tt.want)
			}
		})
	}
}

func TestLoadRefusesAnArgvMissingARequiredPart(t *testing.T) {
	complete := []string{
		"claude", "-p", "{prompt}",
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
		{"no system prompt placeholder", without("{system_prompt}"), "{system_prompt}"},
		{"no model placeholder", without("{model}"), "{model}"},
	}
	for _, tt := range tests {
		t.Run(tt.name+" in agent_command", func(t *testing.T) {
			_, err := config.Load(writeConfig(t, agentCommandLine(t, tt.argv)), testEnv)
			if err == nil || !strings.Contains(err.Error(), tt.missing) {
				t.Errorf("Load error = %v, want one naming %s", err, tt.missing)
			}
		})
		t.Run(tt.name+" in CC_AGENT_COMMAND", func(t *testing.T) {
			raw, err := json.Marshal(tt.argv)
			if err != nil {
				t.Fatal(err)
			}
			env := testEnv
			env.AgentCommand = string(raw)

			_, err = config.Load(writeConfig(t, agentCommandLine(t, complete)), env)
			if err == nil || !strings.Contains(err.Error(), tt.missing) {
				t.Errorf("Load error = %v, want one naming %s", err, tt.missing)
			}
		})
	}
}

func TestLoadMaxTurns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "unset leaves agent_command untouched",
			body: "",
			want: []string{
				"claude", "-p", "{prompt}",
				"--output-format", "stream-json", "--verbose",
				"--settings", "{settings}",
				"--append-system-prompt-file", "{system_prompt}",
				"--permission-mode", "auto",
				"--model", "{model}",
				"{session}",
			},
		},
		{
			name: "set appends --max-turns with the configured value",
			body: "max_turns = 40\n",
			want: []string{
				"claude", "-p", "{prompt}",
				"--output-format", "stream-json", "--verbose",
				"--settings", "{settings}",
				"--append-system-prompt-file", "{system_prompt}",
				"--permission-mode", "auto",
				"--model", "{model}",
				"{session}",
				"--max-turns", "40",
			},
		},
		{
			name: "an explicit empty agent_command stays empty so spawning stays off",
			body: "max_turns = 40\nagent_command = []\n",
			want: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := config.Load(writeConfig(t, tt.body), testEnv)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if !slices.Equal(got.AgentCommand, tt.want) {
				t.Errorf("agent_command = %q, want %q", got.AgentCommand, tt.want)
			}
		})
	}
}

// TestAgentCommandEnvOverridesTheTrackedOne covers phase 5: the config is tracked and the same
// on every machine, so a local wrapper (caffeinate, a sandbox) arrives by environment.
func TestAgentCommandEnvOverridesTheTrackedOne(t *testing.T) {
	tracked := withRequiredParts("claude", "-p", "{prompt}")
	path := writeConfig(t, agentCommandLine(t, tracked))

	got, err := config.Load(path, testEnv)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.AgentCommand, tracked) {
		t.Errorf("agent_command with no override = %v, want the tracked %v", got.AgentCommand, tracked)
	}

	override := withRequiredParts("caffeinate", "-i", "claude", "-p", "{prompt}")
	raw, err := json.Marshal(override)
	if err != nil {
		t.Fatal(err)
	}
	env := testEnv
	env.AgentCommand = string(raw)
	got, err = config.Load(path, env)
	if err != nil {
		t.Fatal(err)
	}
	if want := override; !slices.Equal(got.AgentCommand, want) {
		t.Errorf("agent_command = %v, want the override %v", got.AgentCommand, want)
	}

	for _, bad := range []string{"caffeinate -i claude", "[]", `{"a":1}`} {
		env.AgentCommand = bad
		if _, err := config.Load(path, env); err == nil {
			t.Errorf("CC_AGENT_COMMAND=%q was accepted, want a refusal", bad)
		}
	}
}

func TestLoadBoardPollSeconds(t *testing.T) {
	t.Parallel()

	got, err := config.Load(writeConfig(t, ""), testEnv)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.BoardPollSeconds != 5 {
		t.Errorf("board_poll_seconds = %d, want default 5", got.BoardPollSeconds)
	}

	got, err = config.Load(writeConfig(t, "board_poll_seconds = 1\n"), testEnv)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.BoardPollSeconds != 1 {
		t.Errorf("board_poll_seconds = %d, want 1", got.BoardPollSeconds)
	}
}

func TestLoadRejectsANonPositiveBoardPoll(t *testing.T) {
	t.Parallel()

	_, err := config.Load(writeConfig(t, "board_poll_seconds = 0\n"), testEnv)
	if err == nil {
		t.Fatal("Load accepted board_poll_seconds = 0")
	}
}

func TestSampleConfigLoads(t *testing.T) {
	t.Parallel()

	if _, err := config.Load("../../docs/command-centre.sample.toml", testEnv); err != nil {
		t.Fatalf("Load(docs/command-centre.sample.toml): %v", err)
	}
}

func TestCheckedInConfigPinsTheDefaultModel(t *testing.T) {
	t.Parallel()

	shipped, err := config.Load("../../cc/config.toml", testEnv)
	if err != nil {
		t.Fatalf("Load(cc/config.toml): %v", err)
	}
	defaults, err := config.Load(writeConfig(t, ""), testEnv)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !slices.Equal(shipped.AgentCommand, defaults.AgentCommand) {
		t.Errorf("cc/config.toml agent_command = %q, want the default %q", shipped.AgentCommand, defaults.AgentCommand)
	}
}

func TestLoadNamesEveryBadField(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want []string
	}{
		{"port and max_agents together", "port = 0\nmax_agents = 0\n", []string{"port", "max_agents"}},
		{"port above range", "port = 70000\n", []string{"port"}},
		{"spend_limit_5h above 100", "spend_limit_5h = 101\n", []string{"spend_limit_5h"}},
		{"spend_limit_5h negative", "spend_limit_5h = -1\n", []string{"spend_limit_5h"}},
		{"board_poll_seconds zero", "board_poll_seconds = 0\n", []string{"board_poll_seconds"}},
		{"max_turns negative", "max_turns = -3\n", []string{"max_turns"}},
		{"argv missing parts", "agent_command = [\"claude\"]\n", []string{"--permission-mode", "{system_prompt}", "{model}"}},
		{
			name: "all at once",
			body: "port = 0\nmax_agents = 0\nspend_limit_5h = 200\nboard_poll_seconds = 0\n" +
				"max_turns = -1\nagent_command = [\"x\"]\n",
			want: []string{"port", "max_agents", "spend_limit_5h", "board_poll_seconds", "max_turns", "--permission-mode"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := config.Load(writeConfig(t, tt.body), testEnv)
			if err == nil {
				t.Fatal("Load error = nil, want one")
			}
			for _, field := range tt.want {
				if !strings.Contains(err.Error(), field) {
					t.Errorf("Load error = %v, want it to name %s", err, field)
				}
			}
		})
	}
}
