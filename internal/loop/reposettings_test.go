package loop_test

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

func TestTheObserverReadsSettingsFromOriginMainEachTickAndNeverFromABranch(t *testing.T) {
	root, repoPath := repoWithOrigin(t)
	pathWithGitAndTpOnly(t)
	cfg, _ := testConfigAndWorkspace(t, root, 1, []string{"true"})
	store := openStore(t)
	trackRepo(t, store, "repo", root+"/remote.git")
	observer := loop.NewObserver(store, &fakeForge{t: t, remote: root + "/remote.git"}, cfg)

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

	pushSettingsFile(t, repoPath, "deny = [\"go.mod\"]\n")
	if got := denyOf(); !slices.Equal(got, []string{"go.mod"}) {
		t.Errorf("deny after the file landed on main = %v, want [go.mod] on the next tick", got)
	}

	runGit(t, "-C", repoPath, "switch", "-q", "-c", "ticket")
	pushSettingsFile(t, repoPath, "deny = []\n")
	runGit(t, "-C", repoPath, "push", "-q", "origin", "ticket")
	if got := denyOf(); !slices.Equal(got, []string{"go.mod"}) {
		t.Errorf("deny after a branch loosened its own file = %v, want [go.mod]: only origin/main counts", got)
	}
}

func TestAnUnreadableSettingsFileSkipsTheRepoAndRecordsTheError(t *testing.T) {
	root, repoPath := repoWithOrigin(t)
	installFakeGh(t, false)

	worktreePath := cutWorktree(t, repoPath, "cc-1")
	commitFile(t, worktreePath, "agent.txt", "agent was here\n")

	store := openStore(t)
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTickets(t.Context(), []storepkg.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	dispositionAsPushed(t, store, ticket.URL, at)

	obs := plan.Observation{
		Worktrees:      map[string]string{plan.BranchKey("repo", "cc-1"): worktreePath},
		PRs:            map[string]plan.PR{},
		SettingsErrors: map[string]string{"repo": "line 2: unknown key \"stackin\""},
	}
	observe := func(context.Context) (plan.Observation, error) { return obs, nil }

	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	lp := loop.NewLoop(store, observe, fixedClock(at), cfg, ws, runner.ProcessRunner{})
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if remoteHasBranch(t, root, "cc-1") {
		t.Error("cc-1 was pushed although its repo's settings could not be read")
	}
	lastErr, found, err := store.LastError(t.Context())
	named := strings.Contains(lastErr.Message, "repo") && strings.Contains(lastErr.Message, "stackin")
	if err != nil || !found || !named {
		t.Errorf("last error = %+v (found %v, err %v), want one naming the repo and the bad key", lastErr, found, err)
	}
}

func TestAPushTouchingTheSettingsFileIsRefusedWhateverTheRepoDenies(t *testing.T) {
	root, repoPath := repoWithOrigin(t)
	installFakeGh(t, false)

	worktreePath := cutWorktree(t, repoPath, "cc-1")
	commitFile(t, worktreePath, config.SettingsFile, "deny = []\n")

	store := openStore(t)
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTickets(t.Context(), []storepkg.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	dispositionAsPushed(t, store, ticket.URL, at)

	obs := plan.Observation{
		Worktrees: map[string]string{plan.BranchKey("repo", "cc-1"): worktreePath}, PRs: map[string]plan.PR{},
		Settings: map[string]config.RepoSettings{"repo": {}},
	}
	observe := func(context.Context) (plan.Observation, error) { return obs, nil }

	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	lp := loop.NewLoop(store, observe, fixedClock(at), cfg, ws, runner.ProcessRunner{})
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if remoteHasBranch(t, root, "cc-1") {
		t.Error("cc-1 was pushed although it changes " + config.SettingsFile)
	}
	events, err := store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, "push_refused", config.SettingsFile) {
		t.Errorf("events = %+v, want a push_refused event naming %s", events, config.SettingsFile)
	}
}
