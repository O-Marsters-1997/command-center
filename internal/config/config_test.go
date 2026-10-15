package config_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/config"
)

var testEnv = config.Env{Home: "/home/test", UserConfigDir: "/home/test/.config"}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "command-centre.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
	return path
}

func TestLoadSettings(t *testing.T) {
	t.Parallel()
	defaultCommand := []string{
		"claude", "-p", "{prompt}",
		"--output-format", "stream-json", "--verbose",
		"--settings", "{settings}",
		"--append-system-prompt-file", "{system_prompt}",
		"--permission-mode", "auto",
		"--model", "{model}",
		"{session}",
	}
	tests := []struct {
		name, body string
		want       config.Config
	}{
		{name: "defaults", want: config.Config{
			Port: 7777, MaxAgents: 1, BoardPollSeconds: 5, AgentCommand: defaultCommand,
		}},
		{name: "spend limit", body: "spend_limit_5h = 80\n", want: config.Config{
			Port: 7777, MaxAgents: 1, SpendLimit5h: 80, BoardPollSeconds: 5, AgentCommand: defaultCommand,
		}},
		{name: "board poll", body: "board_poll_seconds = 1\n", want: config.Config{
			Port: 7777, MaxAgents: 1, BoardPollSeconds: 1, AgentCommand: defaultCommand,
		}},
		{name: "max turns", body: "max_turns = 40\n", want: config.Config{
			Port: 7777, MaxAgents: 1, BoardPollSeconds: 5,
			AgentCommand: append(slices.Clone(defaultCommand), "--max-turns", "40"),
		}},
		{name: "empty command with max turns", body: "max_turns = 40\nagent_command = []\n", want: config.Config{
			Port: 7777, MaxAgents: 1, BoardPollSeconds: 5, AgentCommand: []string{},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := config.Load(writeConfig(t, tt.body), testEnv)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if got.Port != tt.want.Port || got.MaxAgents != tt.want.MaxAgents ||
				got.SpendLimit5h != tt.want.SpendLimit5h || got.BoardPollSeconds != tt.want.BoardPollSeconds {
				t.Errorf("Load(%q) settings = %d/%d/%d/%d, want %d/%d/%d/%d", tt.body,
					got.Port, got.MaxAgents, got.SpendLimit5h, got.BoardPollSeconds,
					tt.want.Port, tt.want.MaxAgents, tt.want.SpendLimit5h, tt.want.BoardPollSeconds)
			}
			if !slices.Equal(got.AgentCommand, tt.want.AgentCommand) {
				t.Errorf("Load(%q) agent_command = %q, want %q", tt.body, got.AgentCommand, tt.want.AgentCommand)
			}
		})
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
		t.Fatalf("Marshal(agent command): %v", err)
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

func TestAgentCommandEnvOverridesTheTrackedOne(t *testing.T) {
	t.Parallel()
	tracked := withRequiredParts("claude", "-p", "{prompt}")
	path := writeConfig(t, agentCommandLine(t, tracked))

	got, err := config.Load(path, testEnv)
	if err != nil {
		t.Fatalf("Load(%s): %v", path, err)
	}
	if !slices.Equal(got.AgentCommand, tracked) {
		t.Errorf("agent_command with no override = %v, want the tracked %v", got.AgentCommand, tracked)
	}

	override := withRequiredParts("caffeinate", "-i", "claude", "-p", "{prompt}")
	raw, err := json.Marshal(override)
	if err != nil {
		t.Fatalf("Marshal(%v): %v", override, err)
	}
	env := testEnv
	env.AgentCommand = string(raw)
	got, err = config.Load(path, env)
	if err != nil {
		t.Fatalf("Load(%s, override): %v", path, err)
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

func TestLoadErrors(t *testing.T) {
	t.Parallel()
	complete := withRequiredParts("claude", "-p", "{prompt}")
	without := func(drop string) []string {
		var argv []string
		for _, arg := range complete {
			if arg != drop {
				argv = append(argv, arg)
			}
		}
		return argv
	}

	type errorCase struct {
		name, body, envCommand string
		want                   []string
	}
	tests := []errorCase{
		{name: "port and max_agents together", body: "port = 0\nmax_agents = 0\n", want: []string{"port", "max_agents"}},
		{name: "port above range", body: "port = 70000\n", want: []string{"port"}},
		{name: "spend_limit_5h above 100", body: "spend_limit_5h = 101\n", want: []string{"spend_limit_5h"}},
		{name: "spend_limit_5h negative", body: "spend_limit_5h = -1\n", want: []string{"spend_limit_5h"}},
		{name: "board_poll_seconds zero", body: "board_poll_seconds = 0\n", want: []string{"board_poll_seconds"}},
		{name: "max_turns negative", body: "max_turns = -3\n", want: []string{"max_turns"}},
		{name: "argv missing parts", body: "agent_command = [\"claude\"]\n",
			want: []string{"--permission-mode", "{system_prompt}", "{model}"}},
		{
			name: "all at once",
			body: "port = 0\nmax_agents = 0\nspend_limit_5h = 200\nboard_poll_seconds = 0\n" +
				"max_turns = -1\nagent_command = [\"x\"]\n",
			want: []string{"port", "max_agents", "spend_limit_5h", "board_poll_seconds", "max_turns", "--permission-mode"},
		},
	}
	for _, part := range []string{"--permission-mode", "{system_prompt}", "{model}"} {
		argv := without(part)
		raw, err := json.Marshal(argv)
		if err != nil {
			t.Fatalf("Marshal(%q): %v", argv, err)
		}
		tests = append(tests,
			errorCase{name: "missing " + part + " in config", body: agentCommandLine(t, argv), want: []string{part}},
			errorCase{
				name: "missing " + part + " in environment", body: agentCommandLine(t, complete),
				envCommand: string(raw), want: []string{part},
			},
		)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			env := testEnv
			env.AgentCommand = tt.envCommand
			_, err := config.Load(writeConfig(t, tt.body), env)
			if err == nil {
				t.Fatal("Load error = nil, want one")
			}
			for _, field := range tt.want {
				found := false
				var joined interface{ Unwrap() []error }
				if !errors.As(err, &joined) {
					t.Fatalf("Load error = %v, want joined field errors", err)
				}
				for _, cause := range joined.Unwrap() {
					var invalid *config.InvalidValueError
					var missing *config.MissingKeyError
					if errors.As(cause, &invalid) && invalid.Key == field ||
						errors.As(cause, &missing) && missing.Key == field {
						found = true
					}
				}
				if !found {
					t.Errorf("Load error = %v, want invalid or missing %s", err, field)
				}
			}
		})
	}
}
