package loop_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cctest"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

type stubbornRunner struct{ *runner.Fake }

func (stubbornRunner) Cancel(int) error { return nil }

type steerFixture struct {
	store     *storepkg.Store
	dsn       string
	fake      *runner.Fake
	loop      *loop.Loop
	ticket    storepkg.Ticket
	sessionID string
	repoPath  string
	at        time.Time
}

func newSteerFixture(t *testing.T, wrap func(*runner.Fake) runner.Runner) *steerFixture {
	t.Helper()
	root, repoPath := repoWithOrigin(t)
	worktreePath := filepath.Join(t.TempDir(), "wt")
	runGit(t, "-C", repoPath, "worktree", "add", "-b", "cc-1", worktreePath, "origin/main")
	baseline := strings.TrimSpace(runGitOutput(t, "-C", repoPath, "rev-parse", "refs/heads/cc-1"))
	runGit(t, "-C", worktreePath, "commit", "-q", "--allow-empty", "-m", "agent work")

	dsn := cctest.DSN(t)
	store := openStoreAt(t, dsn)
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTickets(t.Context(), []storepkg.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	runID, sessionID, err := store.InsertSessionRunSkeleton(
		t.Context(), ticket.URL, "agent", baseline, "hash-1", storepkg.RunSession{StartNew: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordSpawn(t.Context(), runID, 999, at, "/state/runs/1.jsonl"); err != nil {
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
		Worktrees: map[string]string{plan.BranchKey("repo", "cc-1"): worktreePath}, PRs: map[string]plan.PR{},
	}
	observe := func(context.Context) (plan.Observation, error) { return obs, nil }
	fake := runner.NewFake()
	fake.Alive[999] = true
	var rn runner.Runner = fake
	if wrap != nil {
		rn = wrap(fake)
	}
	return &steerFixture{
		store: store, dsn: dsn, fake: fake, ticket: ticket, sessionID: sessionID, repoPath: repoPath, at: at,
		loop: loop.NewLoop(store, observe, fixedClock(at.Add(30*time.Second)), cfg, ws, rn),
	}
}

func (f *steerFixture) send(t *testing.T, text string) {
	t.Helper()
	if err := f.store.QueueVerbIntentWithPayload(t.Context(), f.ticket.URL, plan.VerbFollowUp, text, f.at); err != nil {
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
	if !strings.Contains(spawn.Prompt, "use sqlite instead") || !strings.Contains(spawn.Prompt, "cut off") {
		t.Errorf("prompt = %q, want the interrupt wrapper around the message", spawn.Prompt)
	}
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
	if got := runGitOutput(t, "-C", f.repoPath, "ls-remote", "origin", "refs/heads/cc-1"); strings.TrimSpace(got) != "" {
		t.Errorf("origin carries cc-1 (%q), want a continued run with commits not pushed", got)
	}
}

func TestTheInterruptedRunIsRecordedAsContinued(t *testing.T) {
	f := newSteerFixture(t, nil)
	f.send(t, "use sqlite instead")

	f.tick(t)

	db, err := sql.Open("pgx", f.dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var outcome string
	if err := db.QueryRow(`SELECT outcome FROM runs ORDER BY id LIMIT 1`).Scan(&outcome); err != nil {
		t.Fatal(err)
	}
	if outcome != plan.OutcomeContinued.String() {
		t.Errorf("first run outcome = %q, want %q", outcome, plan.OutcomeContinued)
	}
}

func TestARunThatSurvivesTheCancelKeepsTheFollowUpPending(t *testing.T) {
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
