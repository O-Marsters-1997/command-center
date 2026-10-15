package loop_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/gittest"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

type stubbornRunner struct{ *runner.Fake }

func (stubbornRunner) Cancel(int) error { return nil }

type steerFixture struct {
	store     *storepkg.Store
	fake      *runner.Fake
	loop      *loop.Loop
	ticket    storepkg.Ticket
	sessionID string
	repoPath  string
	runsDir   string
	at        time.Time
}

func newSteerFixture(t *testing.T, wrap func(*runner.Fake) runner.Runner) *steerFixture {
	t.Helper()
	root, repoPath := repoWithOrigin(t)
	worktreePath := filepath.Join(t.TempDir(), "wt")
	gittest.Run(t, repoPath, "worktree", "add", "-b", "cc-1", worktreePath, "origin/main")
	baseline := gittest.Run(t, repoPath, "rev-parse", "refs/heads/cc-1")
	gittest.Run(t, worktreePath, "commit", "-q", "--allow-empty", "-m", "agent work")

	store := openStore(t)
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTickets(t.Context(), []storepkg.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}
	at := testAt
	runID, sessionID, err := store.InsertSessionRunSkeleton(t.Context(), storepkg.RunSkeleton{
		Ticket: ticket.URL, Kind: "agent", BaselineSHA: baseline, PromptHash: "hash-1",
		Session: storepkg.RunSession{StartNew: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	spawn := storepkg.Spawn{RunID: plan.RunID(runID), Pgid: 999, StartedAt: at, LogPath: "/state/runs/1.jsonl"}
	if err := store.RecordSpawn(t.Context(), spawn); err != nil {
		t.Fatal(err)
	}

	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	cfg.ClaudeProjectsDir = t.TempDir()
	project := filepath.Join(cfg.ClaudeProjectsDir, "-some-project")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, sessionID+".jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	obs := plan.Observation{
		Worktrees: map[plan.BranchRef]string{branchRef("repo", "cc-1"): worktreePath}, PRs: map[plan.BranchRef]plan.PR{},
	}
	observe := func(context.Context) (plan.Observation, error) { return obs, nil }
	fake := runner.NewFake()
	fake.Alive[999] = true
	var rn runner.Runner = fake
	if wrap != nil {
		rn = wrap(fake)
	}
	return &steerFixture{
		store: store, fake: fake, ticket: ticket, sessionID: sessionID, repoPath: repoPath, runsDir: ws.RunsDir, at: at,
		loop: newLoop(store, observe, fixedClock(at.Add(30*time.Second)), cfg, ws, rn),
	}
}

func (f *steerFixture) send(t *testing.T, text string) {
	t.Helper()
	err := f.store.QueueVerbIntentWithPayload(t.Context(), storepkg.VerbRequest{
		Ticket: f.ticket.URL, Verb: plan.VerbFollowUp, Payload: text, At: f.at,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func (f *steerFixture) tick(t *testing.T) {
	t.Helper()
	if err := f.loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
}

func (f *steerFixture) pendingFollowUps(t *testing.T) int {
	t.Helper()
	pending, err := f.store.PendingVerbIntents(t.Context(), plan.VerbFollowUp)
	if err != nil {
		t.Fatal(err)
	}
	return len(pending)
}

func TestAFollowUpOnARunningTicketCancelsDisposesContinuedAndResumesInTheSameTick(t *testing.T) {
	t.Parallel()
	f := newSteerFixture(t, nil)
	f.send(t, "use sqlite instead")

	f.tick(t)

	if len(f.fake.Canceled) != 1 || f.fake.Canceled[0] != 999 {
		t.Errorf("canceled = %v, want the live run's pgid 999", f.fake.Canceled)
	}
	if len(f.fake.Spawns) != 1 {
		t.Fatalf("spawns = %d, want 1 resume", len(f.fake.Spawns))
	}
	spawn := f.fake.Spawns[0]
	if !spawn.Resume || spawn.SessionID != f.sessionID {
		t.Errorf("spawn = resume %v session %q, want a resume of %q", spawn.Resume, spawn.SessionID, f.sessionID)
	}
	assertPromptGolden(t, "testdata/prompt-steer-interrupt.golden.txt", spawn.Prompt, f.runsDir)
	if got := f.pendingFollowUps(t); got != 0 {
		t.Errorf("pending follow-ups = %d, want the intent consumed", got)
	}

	ids, err := f.store.RunIDsForTicket(t.Context(), f.ticket.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 {
		t.Fatalf("runs = %v, want the interrupted run and its resume", ids)
	}
	if got := gittest.Run(t, f.repoPath, "ls-remote", "origin", "refs/heads/cc-1"); got != "" {
		t.Errorf("origin carries cc-1 (%q), want a continued run with commits not pushed", got)
	}
}

func TestTheInterruptedRunIsRecordedAsContinued(t *testing.T) {
	t.Parallel()
	f := newSteerFixture(t, nil)
	f.send(t, "use sqlite instead")

	f.tick(t)

	ids, err := f.store.RunIDsForTicket(t.Context(), f.ticket.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 {
		t.Fatalf("runs = %v, want 2", ids)
	}
	first, err := f.store.RunDetails(t.Context(), plan.RunID(ids[0]))
	if err != nil {
		t.Fatal(err)
	}
	if !first.Outcome.IsSome() || first.Outcome.Unwrap() != plan.OutcomeContinued {
		t.Errorf("first run outcome = %v, want %q", first.Outcome, plan.OutcomeContinued)
	}
}

func TestARunThatSurvivesTheCancelKeepsTheFollowUpPending(t *testing.T) {
	t.Parallel()
	f := newSteerFixture(t, func(fake *runner.Fake) runner.Runner { return stubbornRunner{fake} })
	f.send(t, "use sqlite instead")

	f.tick(t)

	if len(f.fake.Spawns) != 0 {
		t.Fatalf("spawns = %d, want none while the run is alive", len(f.fake.Spawns))
	}
	if got := f.pendingFollowUps(t); got != 1 {
		t.Fatalf("pending follow-ups = %d, want the intent kept for the next tick", got)
	}

	f.fake.Alive[999] = false
	f.tick(t)

	if len(f.fake.Spawns) != 1 || !f.fake.Spawns[0].Resume {
		t.Errorf("spawns = %+v, want one resume once the run is gone", f.fake.Spawns)
	}
	if got := f.pendingFollowUps(t); got != 0 {
		t.Errorf("pending follow-ups = %d, want the intent consumed", got)
	}
}

func TestTwoMessagesBeforeATickEachProduceTheirOwnResumeInOrder(t *testing.T) {
	t.Parallel()
	f := newSteerFixture(t, nil)
	f.send(t, "first message")
	f.send(t, "second message")

	f.tick(t)

	if len(f.fake.Spawns) != 1 || !strings.Contains(f.fake.Spawns[0].Prompt, "first message") {
		t.Fatalf("spawns = %+v, want only the first message resumed", f.fake.Spawns)
	}
	if got := f.pendingFollowUps(t); got != 1 {
		t.Fatalf("pending follow-ups = %d, want the second message kept", got)
	}

	f.tick(t)

	if len(f.fake.Spawns) != 2 || !strings.Contains(f.fake.Spawns[1].Prompt, "second message") {
		t.Fatalf("spawns = %+v, want the second message resumed next", f.fake.Spawns)
	}
	if !f.fake.Spawns[1].Resume || f.fake.Spawns[1].SessionID != f.sessionID {
		t.Errorf("second spawn = %+v, want a resume of the same session", f.fake.Spawns[1])
	}
	if got := f.pendingFollowUps(t); got != 0 {
		t.Errorf("pending follow-ups = %d, want none", got)
	}
}
