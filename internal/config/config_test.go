package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/verdict"
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
	if len(got.Repos) != 1 || got.Repos[0].Name != "cc-sandbox" {
		t.Errorf("repos = %+v", got.Repos)
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
		"--model", "claude-sonnet-5",
	}
	if !slices.Equal(got.AgentCommand, want) {
		t.Errorf("agent_command = %q, want default %q", got.AgentCommand, want)
	}
	if got.Repos[0].Tracker != "github" {
		t.Errorf("tracker = %q, want default github for a [[repo]] naming none", got.Repos[0].Tracker)
	}
}

func TestLoadConfigKeepsAnExplicitTracker(t *testing.T) {
	t.Parallel()

	body := "[[repo]]\nname = \"r\"\npath = \"r\"\ntracker = \"linear\"\n"
	got, err := config.LoadConfig(writeConfig(t, body))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got.Repos[0].Tracker != "linear" {
		t.Errorf("tracker = %q, want the configured linear left untouched", got.Repos[0].Tracker)
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
				"--model", "claude-sonnet-5",
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
				"--model", "claude-sonnet-5",
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

const oneRepoWithChecks = `
[[repo]]
name        = "r"
path        = "r"
mergify_sha = "sha256:deadbeef"

  [repo.checks]
  all_of = [
    { success = "Lint" },
    { any_of = [
        { success = "verify / Linear issue is linked" },
        { author = "dependabot[bot]" },
    ] },
  ]
`

// TestLoadConfigParsesChecks decodes [repo.checks] straight into verdict.Predicate — the same
// struct internal/verdict.Evaluate takes, with no intermediate DTO.
func TestLoadConfigParsesChecks(t *testing.T) {
	t.Parallel()

	got, err := config.LoadConfig(writeConfig(t, oneRepoWithChecks))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(got.Repos) != 1 {
		t.Fatalf("repos = %+v", got.Repos)
	}
	repo := got.Repos[0]
	if repo.MergifySHA != "sha256:deadbeef" {
		t.Errorf("mergify_sha = %q", repo.MergifySHA)
	}
	if repo.Checks.IsZero() {
		t.Fatal("checks decoded as zero-value")
	}
	if len(repo.Checks.AllOf) != 2 {
		t.Fatalf("all_of = %+v, want 2 entries", repo.Checks.AllOf)
	}
	if repo.Checks.AllOf[0].Success != "Lint" {
		t.Errorf("all_of[0] = %+v", repo.Checks.AllOf[0])
	}
	anyOf := repo.Checks.AllOf[1].AnyOf
	if len(anyOf) != 2 || anyOf[1].Author != "dependabot[bot]" {
		t.Errorf("all_of[1].any_of = %+v", anyOf)
	}
}

// TestLoadConfigResolvesRepoPathsAgainstTheConfigFile covers phase 3: a relative path is
// relative to the directory the config file is in, and an absolute one is taken as written.
func TestLoadConfigResolvesRepoPathsAgainstTheConfigFile(t *testing.T) {
	t.Parallel()

	elsewhere := t.TempDir()
	path := writeConfig(t, "[[repo]]\nname = \"rel\"\npath = \"checkouts/rel\"\n\n"+
		"[[repo]]\nname = \"abs\"\npath = "+strconv.Quote(elsewhere)+"\n")

	got, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if want := filepath.Join(filepath.Dir(path), "checkouts", "rel"); got.Repos[0].Checkout != want {
		t.Errorf("relative checkout = %q, want %q", got.Repos[0].Checkout, want)
	}
	if got.Repos[1].Checkout != elsewhere {
		t.Errorf("absolute checkout = %q, want %q", got.Repos[1].Checkout, elsewhere)
	}
}

func TestLoadConfigRefusesARepoWithNoPath(t *testing.T) {
	t.Parallel()

	_, err := config.LoadConfig(writeConfig(t, "[[repo]]\nname = \"r\"\n"))
	if err == nil || !strings.Contains(err.Error(), "r") {
		t.Errorf("error = %v, want one naming the repo with no path", err)
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

func TestPlanRulesIndexesEachRepoByName(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		MaxAgents: 3, SpendLimit5h: 80,
		Repos: []config.Repo{
			{Name: "a", Stacking: true, Deny: []string{".github/**"}, CompatCheck: "compat", MergifySHA: "sha256:1",
				Checks: verdict.Predicate{Success: "CI"}},
			{Name: "b"},
		},
	}

	rules := cfg.PlanRules()

	if rules.MaxAgents != 3 || rules.SpendLimit5h != 80 {
		t.Errorf("max_agents/spend_limit_5h = %d/%d, want 3/80", rules.MaxAgents, rules.SpendLimit5h)
	}
	if !rules.Stacking["a"] || rules.Stacking["b"] {
		t.Errorf("stacking = %v, want a only", rules.Stacking)
	}
	if !slices.Equal(rules.Deny["a"], []string{".github/**"}) || len(rules.Deny["b"]) != 0 {
		t.Errorf("deny = %v", rules.Deny)
	}
	if rules.Checks["a"].Success != "CI" || !rules.Checks["b"].IsZero() {
		t.Errorf("checks = %v", rules.Checks)
	}
	if rules.MergifySHA["a"] != "sha256:1" || rules.CompatCheck["a"] != "compat" {
		t.Errorf("mergify/compat = %v/%v", rules.MergifySHA, rules.CompatCheck)
	}
	if _, ok := rules.Stacking["b"]; !ok {
		t.Error("a repo that never opted in must still be present, so scope checks see it")
	}
}
