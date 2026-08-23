package cc_test

import (
	"os"
	"path/filepath"
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
