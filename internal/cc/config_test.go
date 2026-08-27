package cc_test

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/cc"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "command-centre.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const twoTasks = `
max_agents = 2
port       = 8080

[[task]]
ticket_url = "sandbox://CC-1"
repo       = "cc-sandbox"
branch     = "cc-1-first"
blocked_by = []

[[task]]
ticket_url = "sandbox://CC-2"
repo       = "cc-sandbox"
branch     = "cc-2-second"
blocked_by = ["sandbox://CC-1"]

[[repo]]
name = "cc-sandbox"
path = "cc-sandbox"
`

func TestLoadConfigTwoTasks(t *testing.T) {
	t.Parallel()

	got, err := cc.LoadConfig(writeConfig(t, twoTasks))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got.MaxAgents != 2 || got.Port != 8080 {
		t.Errorf("max_agents/port = %d/%d, want 2/8080", got.MaxAgents, got.Port)
	}
	if len(got.Tasks) != 2 {
		t.Fatalf("tasks = %d, want 2", len(got.Tasks))
	}
	second := got.Tasks[1]
	if second.TicketURL != "sandbox://CC-2" || second.Branch != "cc-2-second" {
		t.Errorf("second task = %+v", second)
	}
	if len(second.BlockedBy) != 1 || second.BlockedBy[0] != "sandbox://CC-1" {
		t.Errorf("second task blocked_by = %v", second.BlockedBy)
	}
	if len(got.Repos) != 1 || got.Repos[0].Name != "cc-sandbox" {
		t.Errorf("repos = %+v", got.Repos)
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	t.Parallel()

	body := "[[task]]\nticket_url = \"a\"\nrepo = \"r\"\nbranch = \"b\"\n\n[[repo]]\nname = \"r\"\npath = \"r\"\n"
	got, err := cc.LoadConfig(writeConfig(t, body))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got.Port != 7777 {
		t.Errorf("port = %d, want default 7777", got.Port)
	}
	if got.MaxAgents != 1 {
		t.Errorf("max_agents = %d, want default 1", got.MaxAgents)
	}
	want := []string{"claude", "-p", "{prompt}", "--settings", "{settings}", "--model", "claude-sonnet-5"}
	if !slices.Equal(got.AgentCommand, want) {
		t.Errorf("agent_command = %q, want default %q", got.AgentCommand, want)
	}
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
			line: "agent_command = [\"my-agent\", \"--model\", \"claude-opus-5\"]\n",
			want: []string{"my-agent", "--model", "claude-opus-5"},
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

			body := tt.line +
				"[[task]]\nticket_url = \"a\"\nrepo = \"r\"\nbranch = \"b\"\n\n[[repo]]\nname = \"r\"\npath = \"r\"\n"
			got, err := cc.LoadConfig(writeConfig(t, body))
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
[[task]]
ticket_url = "a"
repo       = "r"
branch     = "b"

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
// struct internal/verdict.Evaluate takes, with no intermediate DTO (issue #6).
func TestLoadConfigParsesChecks(t *testing.T) {
	t.Parallel()

	got, err := cc.LoadConfig(writeConfig(t, oneRepoWithChecks))
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

func TestLoadConfigUnknownRepo(t *testing.T) {
	t.Parallel()

	body := "[[task]]\nticket_url = \"a\"\nrepo = \"nope\"\nbranch = \"b\"\n\n[[repo]]\nname = \"r\"\npath = \"r\"\n"
	_, err := cc.LoadConfig(writeConfig(t, body))
	if err == nil {
		t.Fatal("want an error for a task naming a repo with no [[repo]] block")
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Errorf("error %q does not name the missing repo", err)
	}
}

// TestLoadConfigResolvesRepoPathsAgainstTheConfigFile covers phase 3: a relative path is
// relative to the directory the config file is in, and an absolute one is taken as written.
func TestLoadConfigResolvesRepoPathsAgainstTheConfigFile(t *testing.T) {
	t.Parallel()

	elsewhere := t.TempDir()
	path := writeConfig(t, "[[repo]]\nname = \"rel\"\npath = \"checkouts/rel\"\n\n"+
		"[[repo]]\nname = \"abs\"\npath = "+strconv.Quote(elsewhere)+"\n")

	got, err := cc.LoadConfig(path)
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

	_, err := cc.LoadConfig(writeConfig(t, "[[repo]]\nname = \"r\"\n"))
	if err == nil || !strings.Contains(err.Error(), "r") {
		t.Errorf("error = %v, want one naming the repo with no path", err)
	}
}
