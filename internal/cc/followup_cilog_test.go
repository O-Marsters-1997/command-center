package cc_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
	"github.com/O-Marsters-1997/command-center/internal/gh"
	"github.com/O-Marsters-1997/command-center/internal/verdict"
)

const cilogCheckName = "Tests"

// installFakeGhWithLogFailed behaves like installFakeGh, but a `gh run view --log-failed <id>`
// invocation also prints output to stdout, and exits non-zero when fail is true.
func installFakeGhWithLogFailed(t *testing.T, output string, fail bool) (logPath string) {
	t.Helper()
	bin := t.TempDir()
	logPath = filepath.Join(t.TempDir(), "gh.log")
	outPath := filepath.Join(t.TempDir(), "run.log")
	if err := os.WriteFile(outPath, []byte(output), 0o600); err != nil {
		t.Fatal(err)
	}

	exit := "0"
	if fail {
		exit = "1"
	}
	script := "#!/bin/sh\n" +
		"echo \"$*\" >> \"" + logPath + "\"\n" +
		"if [ \"$1 $2 $3\" = \"run view --log-failed\" ]; then\n" +
		"  cat \"" + outPath + "\"\n" +
		"  exit " + exit + "\n" +
		"fi\n" +
		"exit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return logPath
}

// setUpCIFailedTicket wires a ticket whose latest run pushed, whose PR is open at that push's
// tip, and whose sole required check (cilogCheckName) reports conclusion -- FAILURE resolves
// ci_failed, SUCCESS resolves review_me (issue #232 AC3).
func setUpCIFailedTicket(
	t *testing.T, store *cc.Store, root, repoPath string, ticket cc.Ticket, at time.Time, conclusion, detailsURL string,
) (cc.Observation, cc.Config, cc.Workspace) {
	t.Helper()
	ctx := t.Context()

	if err := store.UpsertTickets(ctx, []cc.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}
	dispositionAsPushed(t, store, ticket.URL, at)

	tip, err := cc.BranchTip(ctx, repoPath, ticket.Branch)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordPush(ctx, ticket.URL, tip, "main", tip, at); err != nil {
		t.Fatal(err)
	}

	pr := gh.PR{
		State: gh.Open, HeadOid: tip,
		Checks: map[string]gh.CheckState{
			cilogCheckName: {Status: "COMPLETED", Conclusion: conclusion, DetailsURL: detailsURL},
		},
	}
	obs := cc.Observation{
		PRs:        map[string]gh.PR{cc.BranchKey(ticket.Repo, ticket.Branch): pr},
		BranchTips: map[string]string{cc.MainTipKey(ticket.Repo): tip},
	}

	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	cfg.Repos[0].Checks = verdict.Predicate{Success: cilogCheckName}
	return obs, cfg, ws
}

func TestFollowUpFromCIFailedCarriesLastLinesOfTheFailedLog(t *testing.T) {
	// Not t.Parallel(): installFakeGhWithLogFailed uses t.Setenv.
	root, repoPath := repoWithOrigin(t)
	worktreePath := cutWorktree(t, repoPath, "cc-1")

	var lines []string
	for i := 1; i <= 250; i++ {
		lines = append(lines, fmt.Sprintf("line%d", i))
	}
	installFakeGhWithLogFailed(t, strings.Join(lines, "\n")+"\n", false)

	store := openStore(t)
	ticket := cc.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	const detailsURL = "https://github.com/acme/repo/actions/runs/998877/job/2233"
	obs, cfg, ws := setUpCIFailedTicket(t, store, root, repoPath, ticket, at, "FAILURE", detailsURL)
	obs.Worktrees = map[string]string{cc.BranchKey("repo", "cc-1"): worktreePath}

	if err := store.QueueVerbIntentWithPayload(t.Context(), ticket.URL, "follow-up", "fix it", at); err != nil {
		t.Fatal(err)
	}

	observe := func(context.Context) (cc.Observation, error) { return obs, nil }
	fake := newFakeRunner()
	loop := cc.NewLoop(store, observe, fixedClock(at), cfg, ws, fake)
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if len(fake.spawns) != 1 {
		t.Fatalf("spawns = %d, want 1", len(fake.spawns))
	}
	prompt := fake.spawns[0].Prompt
	if !strings.Contains(prompt, "line250") || !strings.Contains(prompt, "line51") {
		t.Errorf("prompt = %q, want the last 200 lines (51..250)", prompt)
	}
	if strings.Contains(prompt, "line50") || strings.Contains(prompt, "line1\n") {
		t.Errorf("prompt = %q, want no lines before the last 200", prompt)
	}

	events, err := store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if hasEvent(events, "follow_up_ci_log_unavailable", "") {
		t.Errorf("events = %+v, want no unavailable event: the fetch succeeded", events)
	}
}

func TestFollowUpWithNoActionsRunIDSpawnsAnywayNotingLogUnavailable(t *testing.T) {
	// Not t.Parallel(): installFakeGh uses t.Setenv.
	root, repoPath := repoWithOrigin(t)
	worktreePath := cutWorktree(t, repoPath, "cc-1")
	ghLog := installFakeGh(t, false)

	store := openStore(t)
	ticket := cc.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	const externalDetailsURL = "https://example.com/build/123" // a StatusContext, no Actions run id
	obs, cfg, ws := setUpCIFailedTicket(t, store, root, repoPath, ticket, at, "FAILURE", externalDetailsURL)
	obs.Worktrees = map[string]string{cc.BranchKey("repo", "cc-1"): worktreePath}

	if err := store.QueueVerbIntentWithPayload(t.Context(), ticket.URL, "follow-up", "fix it", at); err != nil {
		t.Fatal(err)
	}

	observe := func(context.Context) (cc.Observation, error) { return obs, nil }
	fake := newFakeRunner()
	loop := cc.NewLoop(store, observe, fixedClock(at), cfg, ws, fake)
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if len(fake.spawns) != 1 {
		t.Fatalf("spawns = %d, want 1: a follow-up must spawn even when the log is unavailable", len(fake.spawns))
	}
	if !strings.Contains(fake.spawns[0].Prompt, "could not be retrieved") {
		t.Errorf("prompt = %q, want it to say the log could not be retrieved", fake.spawns[0].Prompt)
	}
	if got := ghLogLines(t, ghLog, "run view"); len(got) != 0 {
		t.Errorf("gh run view invocations = %v, want none: there is no run id to fetch with", got)
	}

	events, err := store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, "follow_up_ci_log_unavailable", "") {
		t.Errorf("events = %+v, want a follow_up_ci_log_unavailable event", events)
	}
}

func TestFollowUpWhenLogFetchFailsSpawnsAnywayNotingLogUnavailable(t *testing.T) {
	// Not t.Parallel(): installFakeGhWithLogFailed uses t.Setenv.
	root, repoPath := repoWithOrigin(t)
	worktreePath := cutWorktree(t, repoPath, "cc-1")
	installFakeGhWithLogFailed(t, "", true) // gh run view --log-failed exits non-zero

	store := openStore(t)
	ticket := cc.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	const detailsURL = "https://github.com/acme/repo/actions/runs/998877/job/2233"
	obs, cfg, ws := setUpCIFailedTicket(t, store, root, repoPath, ticket, at, "FAILURE", detailsURL)
	obs.Worktrees = map[string]string{cc.BranchKey("repo", "cc-1"): worktreePath}

	if err := store.QueueVerbIntentWithPayload(t.Context(), ticket.URL, "follow-up", "fix it", at); err != nil {
		t.Fatal(err)
	}

	observe := func(context.Context) (cc.Observation, error) { return obs, nil }
	fake := newFakeRunner()
	loop := cc.NewLoop(store, observe, fixedClock(at), cfg, ws, fake)
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if len(fake.spawns) != 1 {
		t.Fatalf("spawns = %d, want 1: a follow-up must spawn even when the log fetch fails", len(fake.spawns))
	}
	if !strings.Contains(fake.spawns[0].Prompt, "could not be retrieved") {
		t.Errorf("prompt = %q, want it to say the log could not be retrieved", fake.spawns[0].Prompt)
	}

	events, err := store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, "follow_up_ci_log_unavailable", "") {
		t.Errorf("events = %+v, want a follow_up_ci_log_unavailable event", events)
	}
}

func TestFollowUpFromNonCIFailedStateInjectsNoLog(t *testing.T) {
	// Not t.Parallel(): installFakeGh uses t.Setenv.
	root, repoPath := repoWithOrigin(t)
	worktreePath := cutWorktree(t, repoPath, "cc-1")
	ghLog := installFakeGh(t, false)

	store := openStore(t)
	ticket := cc.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	const detailsURL = "https://github.com/acme/repo/actions/runs/998877/job/2233"
	// The required check passed: this resolves review_me, not ci_failed.
	obs, cfg, ws := setUpCIFailedTicket(t, store, root, repoPath, ticket, at, "SUCCESS", detailsURL)
	obs.Worktrees = map[string]string{cc.BranchKey("repo", "cc-1"): worktreePath}

	if err := store.QueueVerbIntentWithPayload(t.Context(), ticket.URL, "follow-up", "fix it", at); err != nil {
		t.Fatal(err)
	}

	observe := func(context.Context) (cc.Observation, error) { return obs, nil }
	fake := newFakeRunner()
	loop := cc.NewLoop(store, observe, fixedClock(at), cfg, ws, fake)
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if len(fake.spawns) != 1 {
		t.Fatalf("spawns = %d, want 1", len(fake.spawns))
	}
	if strings.Contains(fake.spawns[0].Prompt, "Failed CI log") {
		t.Errorf("prompt = %q, want no CI log section: the ticket is not ci_failed", fake.spawns[0].Prompt)
	}
	if got := ghLogLines(t, ghLog, "run view"); len(got) != 0 {
		t.Errorf("gh run view invocations = %v, want none: a non-ci_failed follow-up must never fetch a log", got)
	}

	events, err := store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if hasEvent(events, "follow_up_ci_log_unavailable", "") {
		t.Errorf("events = %+v, want no unavailable event: no log was ever due", events)
	}
}
