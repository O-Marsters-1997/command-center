package cc_test

import (
	"os"
	"path/filepath"
	"slices"
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

// TestLoadConfigParsesSeamsInConfigOrder covers issue #52's `seams[]`: a task's [[task]] block
// carries the seam names composePrompt later resolves, in the order they were declared.
func TestLoadConfigParsesSeamsInConfigOrder(t *testing.T) {
	t.Parallel()

	body := "[[task]]\nticket_url = \"a\"\nrepo = \"r\"\nbranch = \"b\"\nseams = [\"one\", \"two\"]\n\n" +
		"[[repo]]\nname = \"r\"\npath = \"r\"\n"
	got, err := cc.LoadConfig(writeConfig(t, body))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(got.Tasks) != 1 {
		t.Fatalf("tasks = %+v", got.Tasks)
	}
	if want := []string{"one", "two"}; !slices.Equal(got.Tasks[0].Seams, want) {
		t.Errorf("seams = %v, want %v", got.Tasks[0].Seams, want)
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

// TestLoadConfigParsesSeamBlocks covers issue #58's retirement pointer config: a [[seam]]
// block's producers and lands_at, in declared order.
func TestLoadConfigParsesSeamBlocks(t *testing.T) {
	t.Parallel()

	body := "[[seam]]\n" +
		"name       = \"gql\"\n" +
		"repo       = \"r\"\n" +
		"producers  = [\"sandbox://PRODUCER\"]\n" +
		"lands_at   = [\"schema.graphql\", \"types.graphql\"]\n\n" +
		"[[repo]]\nname = \"r\"\npath = \"r\"\n"
	got, err := cc.LoadConfig(writeConfig(t, body))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(got.Seams) != 1 {
		t.Fatalf("seams = %+v, want 1", got.Seams)
	}
	seam := got.Seams[0]
	if seam.Name != "gql" || seam.Repo != "r" {
		t.Errorf("seam = %+v", seam)
	}
	if want := []string{"sandbox://PRODUCER"}; !slices.Equal(seam.Producers, want) {
		t.Errorf("producers = %v, want %v", seam.Producers, want)
	}
	if want := []string{"schema.graphql", "types.graphql"}; !slices.Equal(seam.LandsAt, want) {
		t.Errorf("lands_at = %v, want %v", seam.LandsAt, want)
	}
}

// TestLoadConfigSeamWithNoLandsAtNeedsNoRepoBlock covers issue #58's AC4: lands_at is optional,
// so a seam declared without it (no retirement) is never required to name a real [[repo]].
func TestLoadConfigSeamWithNoLandsAtNeedsNoRepoBlock(t *testing.T) {
	t.Parallel()

	body := "[[seam]]\nname = \"gql\"\nrepo = \"nope\"\n"
	if _, err := cc.LoadConfig(writeConfig(t, body)); err != nil {
		t.Fatalf("LoadConfig: %v, want no error for a seam with no lands_at", err)
	}
}

func TestLoadConfigSeamWithLandsAtUnknownRepo(t *testing.T) {
	t.Parallel()

	body := "[[seam]]\nname = \"gql\"\nrepo = \"nope\"\nlands_at = [\"schema.graphql\"]\n"
	_, err := cc.LoadConfig(writeConfig(t, body))
	if err == nil {
		t.Fatal("want an error for a retiring seam naming a repo with no [[repo]] block")
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Errorf("error %q does not name the missing repo", err)
	}
}
