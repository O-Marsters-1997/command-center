package config_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/config"
)

var testEnv = config.Env{Home: "/home/test", UserConfigDir: "/home/test/.config"}

var defaultAgentCommand = []string{
	"claude", "-p", "{prompt}",
	"--output-format", "stream-json", "--verbose",
	"--settings", "{settings}",
	"--append-system-prompt-file", "{system_prompt}",
	"--permission-mode", "auto",
	"--model", "{model}",
	"--effort", "{effort}",
	"{session}",
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "command-centre.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
	return path
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

func envWithAgentCommand(t *testing.T, argv []string) config.Env {
	t.Helper()
	raw, err := json.Marshal(argv)
	if err != nil {
		t.Fatalf("Marshal(%v): %v", argv, err)
	}
	env := testEnv
	env.AgentCommand = string(raw)
	return env
}

func TestLoad(t *testing.T) {
	t.Parallel()

	type fields struct {
		Port, MaxAgents, SpendLimit5h, BoardPollSeconds int
		AgentCommand                                    []string
	}
	tests := []struct {
		name string
		body string
		env  config.Env
		want fields
	}{
		{
			name: "defaults",
			want: fields{Port: 7777, MaxAgents: 1, BoardPollSeconds: 5, AgentCommand: defaultAgentCommand},
		},
		{
			name: "an old [[task]] block is ignored",
			body: "max_agents = 2\nport = 8080\n\n[[task]]\nticket_url = \"sandbox://CC-1\"\nrepo = \"cc-sandbox\"\n",
			want: fields{Port: 8080, MaxAgents: 2, BoardPollSeconds: 5, AgentCommand: defaultAgentCommand},
		},
		{
			name: "spend_limit_5h",
			body: "spend_limit_5h = 80\n",
			want: fields{Port: 7777, MaxAgents: 1, SpendLimit5h: 80, BoardPollSeconds: 5, AgentCommand: defaultAgentCommand},
		},
		{
			name: "board_poll_seconds",
			body: "board_poll_seconds = 1\n",
			want: fields{Port: 7777, MaxAgents: 1, BoardPollSeconds: 1, AgentCommand: defaultAgentCommand},
		},
		{
			name: "a configured agent_command replaces the default outright",
			body: agentCommandLine(t, withRequiredParts("my-agent", "--model", "claude-opus-5")),
			want: fields{
				Port: 7777, MaxAgents: 1, BoardPollSeconds: 5,
				AgentCommand: withRequiredParts("my-agent", "--model", "claude-opus-5"),
			},
		},
		{
			name: "an explicit empty agent_command stays empty",
			body: "agent_command = []\n",
			want: fields{Port: 7777, MaxAgents: 1, BoardPollSeconds: 5, AgentCommand: []string{}},
		},
		{
			name: "max_turns appends --max-turns",
			body: "max_turns = 40\n",
			want: fields{
				Port: 7777, MaxAgents: 1, BoardPollSeconds: 5,
				AgentCommand: append(slices.Clone(defaultAgentCommand), "--max-turns", "40"),
			},
		},
		{
			name: "max_turns leaves an empty agent_command empty",
			body: "max_turns = 40\nagent_command = []\n",
			want: fields{Port: 7777, MaxAgents: 1, BoardPollSeconds: 5, AgentCommand: []string{}},
		},
		{
			name: "CC_AGENT_COMMAND overrides the configured one",
			body: agentCommandLine(t, withRequiredParts("claude", "-p", "{prompt}")),
			env:  envWithAgentCommand(t, withRequiredParts("caffeinate", "-i", "claude", "-p", "{prompt}")),
			want: fields{
				Port: 7777, MaxAgents: 1, BoardPollSeconds: 5,
				AgentCommand: withRequiredParts("caffeinate", "-i", "claude", "-p", "{prompt}"),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := tt.env
			if env == (config.Env{}) {
				env = testEnv
			}

			cfg, err := config.Load(writeConfig(t, tt.body), env)
			if err != nil {
				t.Fatalf("Load(%q) error = %v", tt.body, err)
			}
			got := fields{
				Port: cfg.Port, MaxAgents: cfg.MaxAgents, SpendLimit5h: cfg.SpendLimit5h,
				BoardPollSeconds: cfg.BoardPollSeconds, AgentCommand: cfg.AgentCommand,
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Load(%q) mismatch (-want +got):\n%s", tt.body, diff)
			}
		})
	}

	t.Run("an argv missing a required part", func(t *testing.T) {
		t.Parallel()
		complete := withRequiredParts("claude", "-p", "{prompt}")
		without := func(drop string) []string {
			return slices.DeleteFunc(slices.Clone(complete), func(a string) bool { return a == drop })
		}
		for _, missing := range []string{"--permission-mode", "{system_prompt}", "{model}"} {
			t.Run(missing+" in agent_command", func(t *testing.T) {
				t.Parallel()
				_, err := config.Load(writeConfig(t, agentCommandLine(t, without(missing))), testEnv)
				if !errors.Is(err, &config.MissingKeyError{Key: missing}) {
					t.Errorf("Load(agent_command without %s) error = %v, want missing key %s", missing, err, missing)
				}
			})
			t.Run(missing+" in CC_AGENT_COMMAND", func(t *testing.T) {
				t.Parallel()
				_, err := config.Load(writeConfig(t, agentCommandLine(t, complete)), envWithAgentCommand(t, without(missing)))
				if !errors.Is(err, &config.MissingKeyError{Key: missing}) {
					t.Errorf("Load(CC_AGENT_COMMAND without %s) error = %v, want missing key %s", missing, err, missing)
				}
			})
		}
	})

	t.Run("a malformed CC_AGENT_COMMAND", func(t *testing.T) {
		t.Parallel()
		path := writeConfig(t, agentCommandLine(t, withRequiredParts("claude", "-p", "{prompt}")))
		for _, bad := range []string{"caffeinate -i claude", "[]", `{"a":1}`} {
			env := testEnv
			env.AgentCommand = bad
			if _, err := config.Load(path, env); err == nil {
				t.Errorf("Load(CC_AGENT_COMMAND=%q) error = nil, want one", bad)
			}
		}
	})

	t.Run("names every bad field", func(t *testing.T) {
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
			{
				"argv missing parts", "agent_command = [\"claude\"]\n",
				[]string{"--permission-mode", "{system_prompt}", "{model}"},
			},
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
					t.Fatalf("Load(%q) error = nil, want one", tt.body)
				}
				for _, field := range tt.want {
					if !errors.Is(err, &config.InvalidValueError{Key: field}) && !errors.Is(err, &config.MissingKeyError{Key: field}) {
						t.Errorf("Load(%q) error = %v, want invalid or missing %s", tt.body, err, field)
					}
				}
			})
		}
	})
}

func TestCheckoutPath(t *testing.T) {
	t.Parallel()

	got := config.CheckoutPath("/data", "O-Marsters-1997/command-center")
	if want := filepath.Join("/data", "repos", "O-Marsters-1997", "command-center"); got != want {
		t.Errorf("CheckoutPath(/data, O-Marsters-1997/command-center) = %q, want %q", got, want)
	}
}
