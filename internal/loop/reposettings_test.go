package loop_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/gittest"
	"github.com/O-Marsters-1997/command-center/internal/loop"
)

func TestTheObserverReadsSettingsFromOriginMainEachTickAndNeverFromABranch(t *testing.T) {
	t.Parallel()
	f := newPushFixture(t)
	cfg, _ := testConfigAndWorkspace(t, f.root, 1, []string{"true"})
	store := f.Store
	trackRepo(t, store, "repo", f.Origin)
	observer := loop.NewObserver(store, &recordingForge{}, cfg)

	denyOf := func() []string {
		t.Helper()
		obs, err := observer(t.Context())
		if err != nil {
			t.Fatalf("observe: %v", err)
		}
		return obs.Settings["repo"].Deny
	}

	if got := denyOf(); len(got) != 0 {
		t.Fatalf("deny with no settings file = %v, want none", got)
	}

	pushSettingsFile(t, f.Repo, "deny = [\"go.mod\"]\n")
	if got := denyOf(); !slices.Equal(got, []string{"go.mod"}) {
		t.Errorf("deny after the file landed on main = %v, want [go.mod] on the next tick", got)
	}

	gittest.Run(t, f.Repo, "switch", "-q", "-c", "ticket")
	pushSettingsFile(t, f.Repo, "deny = []\n")
	gittest.Run(t, f.Repo, "push", "-q", "origin", "ticket")
	if got := denyOf(); !slices.Equal(got, []string{"go.mod"}) {
		t.Errorf("deny after a branch loosened its own file = %v, want [go.mod]: only origin/main counts", got)
	}
}

func TestAnUnreadableSettingsFileSkipsTheRepoAndRecordsTheError(t *testing.T) {
	t.Parallel()
	f := newPushFixture(t, loop.WithWorktrees(&recordingWorktrees{}))
	gittest.Commit(t, f.Worktree, "agent.txt", "agent was here\n")
	dispositionAsPushed(t, f.Store, f.Ticket.URL, testAt)
	f.Obs.SettingsErrors = map[string]string{"repo": "line 2: unknown key \"stackin\""}
	f.tick(t)

	if gittest.HasRef(t, f.Origin, "refs/heads/cc-1") {
		t.Error("cc-1 was pushed although its repo's settings could not be read")
	}
	lastErr, err := f.Store.LastError(t.Context())
	if err != nil || lastErr.IsNone() {
		t.Fatalf("last error = %v (err %v), want one naming the repo and the bad key", lastErr, err)
	}
	if msg := lastErr.Unwrap().Message; !strings.Contains(msg, "repo") || !strings.Contains(msg, "stackin") {
		t.Errorf("last error = %q, want one naming the repo and the bad key", msg)
	}
}

func TestAPushTouchingTheSettingsFileIsRefusedWhateverTheRepoDenies(t *testing.T) {
	t.Parallel()
	f := newPushFixture(t, loop.WithWorktrees(&recordingWorktrees{}))
	gittest.Commit(t, f.Worktree, config.SettingsFile, "deny = []\n")
	dispositionAsPushed(t, f.Store, f.Ticket.URL, testAt)
	f.Obs.Settings = map[string]config.RepoSettings{"repo": {}}
	f.tick(t)

	if gittest.HasRef(t, f.Origin, "refs/heads/cc-1") {
		t.Error("cc-1 was pushed although it changes " + config.SettingsFile)
	}
	events := f.events(t, "push_refused")
	if len(events) != 1 || events[0].TicketURL != f.Ticket.URL ||
		!strings.Contains(events[0].Detail, config.SettingsFile) {
		t.Errorf("events = %+v, want a push_refused event naming %s", events, config.SettingsFile)
	}
}
