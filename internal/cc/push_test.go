package cc_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
	"github.com/O-Marsters-1997/command-center/internal/gh"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

// installFakeGh puts a script named gh on PATH that logs every invocation to a file and answers
// `pr create` per the caller's control: failCreate makes it exit non-zero, exercising push
// failed without a real GitHub call. `issue view` always answers with a canned body, which is
// what lets a spawn's prompt-composition step run against a fake ticket URL.
func installFakeGh(t *testing.T, failCreate bool) (logPath string) {
	t.Helper()
	bin := t.TempDir()
	logPath = filepath.Join(t.TempDir(), "gh.log")

	fail := "0"
	if failCreate {
		fail = "1"
	}
	script := "#!/bin/sh\n" +
		"echo \"$*\" >> \"" + logPath + "\"\n" +
		"if [ \"$1 $2\" = \"pr create\" ] && [ \"" + fail + "\" = \"1\" ]; then\n" +
		"  echo 'fake gh: pr create failed' >&2\n" +
		"  exit 1\n" +
		"fi\n" +
		"if [ \"$1 $2\" = \"issue view\" ]; then\n" +
		"  echo \"${CC_FAKE_ISSUE_BODY:-fake ticket body}\"\n" +
		"fi\n" +
		"exit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return logPath
}

// cutWorktree creates a real worktree for branch off origin/main, the shape pushOne's diff and
// push both need without going through tp new (already covered by loop_launch_test.go).
func cutWorktree(t *testing.T, repoPath, branch string) string {
	t.Helper()
	worktreePath := filepath.Join(t.TempDir(), "wt-"+branch)
	runGit(t, "-C", repoPath, "worktree", "add", "-b", branch, worktreePath, "origin/main")
	return worktreePath
}

func commitFile(t *testing.T, worktreePath, relPath, contents string) {
	t.Helper()
	full := filepath.Join(worktreePath, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, "-C", worktreePath, "add", relPath)
	runGit(t, "-C", worktreePath, "commit", "-q", "-m", "agent: touch "+relPath)
}

// dispositionAsPushed records a run whose disposition is already known to be push, so these
// tests can drive pushPushable directly without re-exercising spawn/dispose (loop_launch_test.go
// already covers that).
func dispositionAsPushed(t *testing.T, store *cc.Store, ticketURL string, at time.Time) {
	t.Helper()
	runID, err := store.InsertRunSkeleton(t.Context(), ticketURL, "agent", "", "hash-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordSpawn(t.Context(), runID, 111, at, "/state/runs/1.jsonl"); err != nil {
		t.Fatal(err)
	}
	exitCode := 0
	if err := store.RecordDisposition(t.Context(), runID, plan.OutcomePush, &exitCode, at); err != nil {
		t.Fatal(err)
	}
}

func countEvents(events []cc.Event, kind string) int {
	n := 0
	for _, e := range events {
		if e.Kind == kind {
			n++
		}
	}
	return n
}

func hasEvent(events []cc.Event, kind, detailSubstring string) bool {
	for _, e := range events {
		if e.Kind == kind && strings.Contains(e.Detail, detailSubstring) {
			return true
		}
	}
	return false
}

func remoteHasBranch(t *testing.T, root, branch string) bool {
	t.Helper()
	remote := filepath.Join(root, "remote.git")
	cmd := exec.Command("git", "-C", remote, "rev-parse", "--verify", "refs/heads/"+branch)
	return cmd.Run() == nil
}

func countLines(t *testing.T, path string) int {
	t.Helper()
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return 0
	}
	return len(strings.Split(trimmed, "\n"))
}

func TestPushPushableRefusesAPolicyHitAndNeverPushes(t *testing.T) {
	// Not t.Parallel(): installFakeGh and repoWithOrigin both use t.Setenv.
	root, repoPath := repoWithOrigin(t)
	installFakeGh(t, false)

	worktreePath := cutWorktree(t, repoPath, "cc-1")
	commitFile(t, worktreePath, ".github/workflows/x.yml", "name: x\n")

	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	ticket := cc.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTickets(t.Context(), []cc.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	dispositionAsPushed(t, store, ticket.URL, at)

	obs := cc.Observation{Worktrees: map[string]string{"cc-1": worktreePath}, PRs: map[string]gh.PR{}}
	observe := func(context.Context) (cc.Observation, error) { return obs, nil }

	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	loop := cc.NewLoop(store, observe, fixedClock(at), cfg, ws, cc.ProcessRunner{})
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if remoteHasBranch(t, root, "cc-1") {
		t.Error("cc-1 must not exist on the remote: the diff hit the push policy")
	}

	facts, err := store.PushFacts(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	f := facts[ticket.URL]
	if !f.Refused || f.RefusedPath != ".github/workflows/x.yml" {
		t.Errorf("push facts = %+v, want refused naming .github/workflows/x.yml", f)
	}

	events, err := store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, "push_refused", ".github/workflows/x.yml") {
		t.Errorf("events = %+v, want a push_refused event naming the path", events)
	}

	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("second RunOnce: %v", err)
	}
	events, err = store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got := countEvents(events, "push_refused"); got != 1 {
		t.Errorf("push_refused events after a second tick = %d, want 1: no auto-retry", got)
	}
}

func TestPushPushablePushesAndCreatesAPROnceThenStaysIdempotent(t *testing.T) {
	// Not t.Parallel(): installFakeGh and repoWithOrigin both use t.Setenv.
	root, repoPath := repoWithOrigin(t)
	ghLog := installFakeGh(t, false)

	worktreePath := cutWorktree(t, repoPath, "cc-1")
	commitFile(t, worktreePath, "agent.txt", "agent was here\n")

	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	ticket := cc.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTickets(t.Context(), []cc.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	dispositionAsPushed(t, store, ticket.URL, at)

	obs := cc.Observation{Worktrees: map[string]string{"cc-1": worktreePath}, PRs: map[string]gh.PR{}}
	observe := func(context.Context) (cc.Observation, error) { return obs, nil }

	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	loop := cc.NewLoop(store, observe, fixedClock(at), cfg, ws, cc.ProcessRunner{})
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if !remoteHasBranch(t, root, "cc-1") {
		t.Fatal("cc-1 was not pushed to the remote")
	}
	if got := countLines(t, ghLog); got != 1 {
		t.Fatalf("gh invocations = %d, want exactly 1 (pr create)", got)
	}
	body, err := os.ReadFile(ghLog)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"pr create", "--base main", "--fill", "--label keep-open"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("gh log %q does not contain %q", body, want)
		}
	}

	tips, err := store.LastPushedTips(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if tips[ticket.URL] == "" {
		t.Error("no pushes row recorded")
	}

	// A second tick with the tip unchanged must not push or create again (inv. 20).
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("second RunOnce: %v", err)
	}
	if got := countLines(t, ghLog); got != 1 {
		t.Errorf("gh invocations after a second tick = %d, want still 1", got)
	}
}

func TestPushPushableAdoptsAnExistingOpenPRRatherThanDuplicating(t *testing.T) {
	// Not t.Parallel(): installFakeGh and repoWithOrigin both use t.Setenv.
	root, repoPath := repoWithOrigin(t)
	ghLog := installFakeGh(t, false)

	worktreePath := cutWorktree(t, repoPath, "cc-1")
	commitFile(t, worktreePath, "agent.txt", "agent was here\n")

	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	ticket := cc.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTickets(t.Context(), []cc.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	dispositionAsPushed(t, store, ticket.URL, at)

	obs := cc.Observation{
		Worktrees: map[string]string{"cc-1": worktreePath},
		PRs:       map[string]gh.PR{"cc-1": {Number: 7, HeadRef: "cc-1", State: gh.Open}},
	}
	observe := func(context.Context) (cc.Observation, error) { return obs, nil }

	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	loop := cc.NewLoop(store, observe, fixedClock(at), cfg, ws, cc.ProcessRunner{})
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if !remoteHasBranch(t, root, "cc-1") {
		t.Error("cc-1 was not pushed to the remote")
	}
	if got := countLines(t, ghLog); got != 0 {
		t.Errorf("gh invocations = %d, want 0: an open PR must be adopted, never duplicated", got)
	}

	tips, err := store.LastPushedTips(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if tips[ticket.URL] == "" {
		t.Error("no pushes row recorded despite adopting the existing PR")
	}
}

func TestPushFailureIsNotRetriedAutomaticallyButRetryPushBypassesTheGate(t *testing.T) {
	// Not t.Parallel(): installFakeGh and repoWithOrigin both use t.Setenv.
	root, repoPath := repoWithOrigin(t)
	ghLog := installFakeGh(t, true) // pr create fails

	worktreePath := cutWorktree(t, repoPath, "cc-1")
	commitFile(t, worktreePath, "agent.txt", "agent was here\n")

	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	ticket := cc.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTickets(t.Context(), []cc.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	dispositionAsPushed(t, store, ticket.URL, at)

	obs := cc.Observation{Worktrees: map[string]string{"cc-1": worktreePath}, PRs: map[string]gh.PR{}}
	observe := func(context.Context) (cc.Observation, error) { return obs, nil }

	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	loop := cc.NewLoop(store, observe, fixedClock(at), cfg, ws, cc.ProcessRunner{})
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if !remoteHasBranch(t, root, "cc-1") {
		t.Fatal("the push itself must still succeed even though pr create fails")
	}
	if got := countLines(t, ghLog); got != 1 {
		t.Fatalf("gh invocations = %d, want 1 (the failed pr create)", got)
	}

	facts, err := store.PushFacts(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !facts[ticket.URL].Failed {
		t.Fatal("push facts must record the failure")
	}
	if tips, _ := store.LastPushedTips(t.Context()); tips[ticket.URL] != "" {
		t.Error("no pushes row until the PR is actually opened")
	}

	// A second automatic tick must not retry: still exactly one gh invocation.
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("second RunOnce: %v", err)
	}
	if got := countLines(t, ghLog); got != 1 {
		t.Errorf("gh invocations after an automatic tick = %d, want still 1: no auto-retry", got)
	}

	// The human's retry-push verb bypasses the gate. Rewrite the fake gh to succeed first.
	if err := os.WriteFile(filepath.Join(filepath.Dir(mustLookPath(t, "gh")), "gh"),
		[]byte("#!/bin/sh\necho \"$*\" >> \""+ghLog+"\"\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := store.QueueVerbIntent(t.Context(), ticket.URL, "retry-push", at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("retry RunOnce: %v", err)
	}
	if got := countLines(t, ghLog); got != 2 {
		t.Fatalf("gh invocations after retry-push = %d, want 2", got)
	}
	if tips, err := store.LastPushedTips(t.Context()); err != nil || tips[ticket.URL] == "" {
		t.Errorf("pushes row missing after a successful retry-push (err=%v)", err)
	}

	pending, err := store.PendingVerbIntents(t.Context(), "retry-push")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Errorf("pending retry-push intents = %+v, want none: consumed", pending)
	}
}

func mustLookPath(t *testing.T, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

// TestPushPushableSkipsATicketWhoseBranchWasRemoved covers the hazard remove-worktree (verbs.go)
// introduces: a ticket's latest run keeps outcome=push forever, so without a guard, pushPushable
// would call BranchTip on it every tick for the rest of the app's life -- and once
// tp remove --force has deleted the branch along with the worktree, that call errors and would
// abort every subsequent tick, for every ticket, not just this one.
func TestPushPushableSkipsATicketWhoseBranchWasRemoved(t *testing.T) {
	// Not t.Parallel(): installFakeGh and repoWithOrigin both use t.Setenv.
	root, repoPath := repoWithOrigin(t)
	installFakeGh(t, false)

	worktreePath := cutWorktree(t, repoPath, "cc-1")
	commitFile(t, worktreePath, "agent.txt", "agent was here\n")
	runGit(t, "-C", repoPath, "push", "-q", "origin", "cc-1")

	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	ticket := cc.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTickets(t.Context(), []cc.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	dispositionAsPushed(t, store, ticket.URL, at)
	// Already fully pushed and recorded, mirroring a merged/base_gone row that has since been
	// torn down: RecordPush's own tip must match, or PushPlan would select it regardless.
	tip := strings.TrimSpace(runGitOutput(t, "-C", repoPath, "rev-parse", "refs/heads/cc-1"))
	if err := store.RecordPush(t.Context(), ticket.URL, tip, "main", tip, at); err != nil {
		t.Fatal(err)
	}

	// The worktree and the branch are both gone -- tp remove --force's real effect.
	runGit(t, "-C", repoPath, "worktree", "remove", "--force", worktreePath)
	runGit(t, "-C", repoPath, "branch", "-D", "cc-1")

	obs := cc.Observation{Worktrees: map[string]string{}, PRs: map[string]gh.PR{}}
	observe := func(context.Context) (cc.Observation, error) { return obs, nil }

	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	loop := cc.NewLoop(store, observe, fixedClock(at), cfg, ws, cc.ProcessRunner{})
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce must not fail once a pushed ticket's branch has been removed: %v", err)
	}
	// A second tick too: the hazard is that every future tick would fail, not just the first.
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("second RunOnce: %v", err)
	}
}
