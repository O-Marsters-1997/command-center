package cc_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
	"github.com/O-Marsters-1997/command-center/internal/gh"
	"github.com/O-Marsters-1997/command-center/internal/verdict"
)

const reCheckCompatCheckName = "GraphQL production compatibility"

// TestReCheckRerunsTheActionsRunParsedFromTheCompatCheckDetailsURL covers issue #56 AC1.
func TestReCheckRerunsTheActionsRunParsedFromTheCompatCheckDetailsURL(t *testing.T) {
	// Not t.Parallel(): installFakeGh uses t.Setenv.
	root, _ := repoWithOrigin(t)
	ghLog := installFakeGh(t, false)

	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	task := cc.Task{TicketURL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTasks(t.Context(), []cc.Task{task}); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	if err := store.QueueVerbIntent(t.Context(), task.TicketURL, "re-check", at); err != nil {
		t.Fatal(err)
	}

	obs := cc.Observation{PRs: map[string]gh.PR{
		"cc-1": {Checks: map[string]gh.CheckState{
			reCheckCompatCheckName: {
				Status: "COMPLETED", Conclusion: "FAILURE",
				DetailsURL: "https://github.com/acme/repo/actions/runs/998877/job/2233",
			},
		}},
	}}
	observe := func(context.Context) (cc.Observation, error) { return obs, nil }

	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	cfg.Repos[0].CompatCheck = reCheckCompatCheckName
	loop := cc.NewLoop(store, observe, fixedClock(at), cfg, ws, cc.ProcessRunner{})
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if got := ghLogLines(t, ghLog, "run rerun"); len(got) != 1 || got[0] != "run rerun 998877" {
		t.Fatalf("gh run rerun invocations = %v, want exactly [\"run rerun 998877\"]", got)
	}

	events, err := store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, "re_check_requested", "") {
		t.Error("no re_check_requested event")
	}

	pending, err := store.PendingVerbIntents(t.Context(), "re-check")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Errorf("pending re-check intents = %+v, want none: consumed", pending)
	}
}

// TestReCheckRefusesADetailsURLWithoutARunsSegment covers issue #56 AC2.
func TestReCheckRefusesADetailsURLWithoutARunsSegment(t *testing.T) {
	// Not t.Parallel(): installFakeGh uses t.Setenv.
	root, _ := repoWithOrigin(t)
	ghLog := installFakeGh(t, false)

	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	task := cc.Task{TicketURL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTasks(t.Context(), []cc.Task{task}); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	if err := store.QueueVerbIntent(t.Context(), task.TicketURL, "re-check", at); err != nil {
		t.Fatal(err)
	}

	const oddDetailsURL = "https://github.com/acme/repo/checks/998877"
	obs := cc.Observation{PRs: map[string]gh.PR{
		"cc-1": {Checks: map[string]gh.CheckState{
			reCheckCompatCheckName: {Status: "COMPLETED", Conclusion: "FAILURE", DetailsURL: oddDetailsURL},
		}},
	}}
	observe := func(context.Context) (cc.Observation, error) { return obs, nil }

	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	cfg.Repos[0].CompatCheck = reCheckCompatCheckName
	loop := cc.NewLoop(store, observe, fixedClock(at), cfg, ws, cc.ProcessRunner{})
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if got := ghLogLines(t, ghLog, "run rerun"); len(got) != 0 {
		t.Fatalf("gh run rerun invocations = %v, want none", got)
	}

	events, err := store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, "re_check_refused", oddDetailsURL) {
		t.Errorf("no re_check_refused event naming %q; events = %+v", oddDetailsURL, events)
	}

	pending, err := store.PendingVerbIntents(t.Context(), "re-check")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Errorf("pending re-check intents = %+v, want none: consumed even on refusal", pending)
	}
}

// TestReCheckResetsTheCheckingWaitSoTheRowReadsCheckingOnceTheRerunIsObserved covers issue #56
// AC1's "returns to checking on the tick after".
func TestReCheckResetsTheCheckingWaitSoTheRowReadsCheckingOnceTheRerunIsObserved(t *testing.T) {
	// Not t.Parallel(): installFakeGh uses t.Setenv.
	root, _ := repoWithOrigin(t)
	installFakeGh(t, false)

	ctx := t.Context()
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	task := cc.Task{TicketURL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTasks(ctx, []cc.Task{task}); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	dispositionAsPushed(t, store, task.TicketURL, at)
	const tip = "cc-1-tip"
	if err := store.RecordPush(ctx, task.TicketURL, tip, "main", "main-tip", at); err != nil {
		t.Fatal(err)
	}

	elapsedTicks := int(verdict.BoundedWait/(15*time.Second)) + 5
	for range elapsedTicks {
		if err := store.IncrementCheckingTicks(ctx, []string{task.TicketURL}); err != nil {
			t.Fatal(err)
		}
	}

	if err := store.QueueVerbIntent(ctx, task.TicketURL, "re-check", at); err != nil {
		t.Fatal(err)
	}

	redPR := gh.PR{
		Number: 1, State: gh.Open, HeadOid: tip,
		Checks: map[string]gh.CheckState{
			reCheckCompatCheckName: {
				Status: "COMPLETED", Conclusion: "FAILURE",
				DetailsURL: "https://github.com/acme/repo/actions/runs/998877/job/2233",
			},
			"Tests": {Status: "COMPLETED", Conclusion: "SUCCESS"},
		},
	}
	obs := cc.Observation{PRs: map[string]gh.PR{"cc-1": redPR}}
	observe := func(context.Context) (cc.Observation, error) { return obs, nil }

	repos := []cc.Repo{{
		Name: "repo", Path: "repo", CompatCheck: reCheckCompatCheckName,
		Checks: verdict.Predicate{AllOf: []verdict.Predicate{
			{Success: reCheckCompatCheckName}, {Success: "Tests"},
		}},
	}}
	ws := cc.Workspace{Root: root, RunsDir: t.TempDir(), SettingsPath: filepath.Join(t.TempDir(), "agent.json")}
	cfg := cc.Config{Repos: repos}
	loop := cc.NewLoop(store, observe, fixedClock(at), cfg, ws, cc.ProcessRunner{})
	if err := loop.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce (re-check tick): %v", err)
	}

	ticks, err := store.CheckingTicks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := ticks[task.TicketURL]; got != 0 {
		t.Fatalf("checking ticks right after re-check = %d, want 0", got)
	}

	pendingPR := redPR
	pendingPR.Checks = map[string]gh.CheckState{
		reCheckCompatCheckName: {Status: "IN_PROGRESS"},
		"Tests":                {Status: "COMPLETED", Conclusion: "SUCCESS"},
	}
	obs = cc.Observation{PRs: map[string]gh.PR{"cc-1": pendingPR}}
	if err := loop.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce (post-rerun tick): %v", err)
	}

	page := renderPage(t, cc.NewServer(store, fixedClock(at), repos))
	if state := rowState(t, page, task.TicketURL); state != "checking" {
		t.Fatalf("state once the rerun's check goes pending = %q, want checking", state)
	}
}
