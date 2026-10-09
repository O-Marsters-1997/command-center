package loop_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func authoriseAsOneLaunch(t *testing.T, f *loopFixture) {
	t.Helper()
	for _, ticket := range f.Tickets {
		hash := plan.Hash(plan.Compose(plan.Ticket{URL: ticket.URL}))
		if err := f.Store.QueueLaunchIntent(t.Context(), ticket.URL, hash, "one-launch", testAt); err != nil {
			t.Fatal(err)
		}
	}
}

func TestALaunchOfThreeTicketsExploresOnceThenImplementsAgainstTheSameBrief(t *testing.T) {
	f := newLoopFixture(t, withMaxAgents(3),
		withTickets(sandboxTicket("1"), sandboxTicket("2"), sandboxTicket("3")))
	authoriseAsOneLaunch(t, f)

	f.Tick(t)
	explores := f.ExploreSpawns()
	if len(explores) != 1 || len(f.Fake.Spawns) != 1 {
		t.Fatalf("spawns = %d (explore %d), want exactly one explore run and nothing else",
			len(f.Fake.Spawns), len(explores))
	}
	prompt, err := os.ReadFile(explores[0].PromptPath)
	if err != nil {
		t.Fatal(err)
	}
	brief := filepath.Join(f.WS.RunsDir, "launch-1", "brief.md")
	for _, want := range []string{brief, "sandbox://CC-1", "sandbox://CC-2", "sandbox://CC-3"} {
		if !strings.Contains(string(prompt), want) {
			t.Errorf("explore prompt lacks %q:\n%s", want, prompt)
		}
	}

	if err := os.WriteFile(brief, []byte("the brief"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.Fake.Alive[1] = false
	f.Tick(t)

	implements := f.ImplementSpawns()
	if len(implements) != 3 {
		t.Fatalf("implement spawns = %d, want 3", len(implements))
	}
	for _, spawn := range implements {
		if spawn.Model != "claude-sonnet-5-5" {
			t.Errorf("implement model = %q, want claude-sonnet-5-5", spawn.Model)
		}
		written, err := os.ReadFile(spawn.PromptPath)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(written), "## Brief") || !strings.Contains(string(written), brief) {
			t.Errorf("implement prompt does not name %s:\n%s", brief, written)
		}
	}
	if explores[0].Model != "claude-haiku-5-5" {
		t.Errorf("explore model = %q, want claude-haiku-5-5", explores[0].Model)
	}
	if got := len(f.ExploreSpawns()); got != 1 {
		t.Errorf("explore spawns after launch = %d, want still 1", got)
	}
}

func TestAFailedExploreRunReleasesTheTicketsWithoutABrief(t *testing.T) {
	f := newLoopFixture(t)
	f.AuthoriseAll(t)
	f.Fake.FailNext = true

	f.Tick(t)
	f.Tick(t)

	implements := f.ImplementSpawns()
	if len(implements) != 1 {
		t.Fatalf("implement spawns = %d, want 1: a failed explore run must not block the launch", len(implements))
	}
	written, err := os.ReadFile(implements[0].PromptPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(written), "## Brief") {
		t.Errorf("implement prompt names a brief that was never written:\n%s", written)
	}
}

func TestADiedExploreRunThatWroteNoBriefReleasesTheTickets(t *testing.T) {
	f := newLoopFixture(t)
	f.AuthoriseAll(t)

	f.TickPastExplore(t)

	if got := len(f.ImplementSpawns()); got != 1 {
		t.Fatalf("implement spawns = %d, want 1", got)
	}
}

func TestAHeldLaunchSpawnsNoImplementRunWhileExploreIsAlive(t *testing.T) {
	f := newLoopFixture(t)
	f.AuthoriseAll(t)

	f.Tick(t)
	f.Tick(t)

	if got := len(f.ImplementSpawns()); got != 0 {
		t.Errorf("implement spawns = %d, want 0 while the explore run is alive", got)
	}
	if got := len(f.ExploreSpawns()); got != 1 {
		t.Errorf("explore spawns = %d, want 1: a held launch must not explore again", got)
	}
}
