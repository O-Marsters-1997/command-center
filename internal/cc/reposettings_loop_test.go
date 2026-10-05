package cc_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
	"github.com/O-Marsters-1997/command-center/internal/gh"
)

// TestLoopSkipsPushForARepoWhoseSettingsFailToParse covers the "What to build" step: a settings
// read error is recorded as the tick's last error, and the repo is skipped for push -- not
// refused, not retried with stale settings, simply not attempted -- while the tick itself still
// completes.
func TestLoopSkipsPushForARepoWhoseSettingsFailToParse(t *testing.T) {
	// Not t.Parallel(): installFakeGh and repoWithOrigin both use t.Setenv.
	root, repoPath := repoWithOrigin(t)
	installFakeGh(t, false)

	writeAndPushSettings(t, repoPath, "deny = not-a-list\n")

	worktreePath := cutWorktree(t, repoPath, "cc-1")
	commitFile(t, worktreePath, "agent.txt", "agent was here\n")

	store := openStore(t)
	ticket := cc.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTickets(t.Context(), []cc.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	dispositionAsPushed(t, store, ticket.URL, at)

	obs := cc.Observation{
		Worktrees: map[string]string{cc.BranchKey("repo", "cc-1"): worktreePath}, PRs: map[string]gh.PR{},
	}
	observe := func(context.Context) (cc.Observation, error) { return obs, nil }

	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	loop := cc.NewLoop(store, observe, fixedClock(at), cfg, ws, cc.ProcessRunner{})
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce must not fail the whole tick over one repo's bad settings: %v", err)
	}

	if remoteHasBranch(t, root, "cc-1") {
		t.Error("cc-1 must not exist on the remote: its repo's settings failed to parse this tick")
	}

	events, err := store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if hasEvent(events, "push_refused", "") || hasEvent(events, "pushed", "") {
		t.Errorf("events = %+v, want no push attempt at all -- skipped, not refused", events)
	}

	lastErr, ok, err := store.LastError(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !ok || !strings.Contains(lastErr.Message, "repo") {
		t.Errorf("last error = %+v, ok=%v, want one naming the repo", lastErr, ok)
	}
}
