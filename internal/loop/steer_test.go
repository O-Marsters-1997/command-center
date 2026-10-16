package loop_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/gittest"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/option"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
	"github.com/O-Marsters-1997/command-center/internal/runner/runnertest"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

type stubbornRunner struct{ *runnertest.Runner }

func (stubbornRunner) Cancel(int) error { return nil }

type steerFixture struct {
	store     *storepkg.Store
	fake      *runnertest.Runner
	loop      *loop.Loop
	ticket    storepkg.Ticket
	sessionID string
	repoPath  string
	runsDir   string
}

func newSteerFixture(t *testing.T, wrap func(*runnertest.Runner) runner.Runner) *steerFixture {
	t.Helper()
	root, repoPath := repoWithOrigin(t)
	worktreePath := filepath.Join(t.TempDir(), "wt")
	gittest.Run(t, repoPath, "worktree", "add", "-b", "cc-1", worktreePath, "origin/main")
	baseline := gittest.Run(t, repoPath, "rev-parse", "refs/heads/cc-1")
	gittest.Run(t, worktreePath, "commit", "-q", "--allow-empty", "-m", "agent work")
	store := openStore(t)
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTickets(t.Context(), []storepkg.Ticket{ticket}); err != nil {
		t.Fatalf("UpsertTickets(%q): %v", ticket.URL, err)
	}
	runID, sessionID, err := store.InsertSessionRunSkeleton(t.Context(), storepkg.RunSkeleton{
		Ticket: ticket.URL, Kind: "agent", BaselineSHA: baseline, PromptHash: "hash-1",
		Session: storepkg.RunSession{StartNew: true},
	})
	if err != nil {
		t.Fatalf("InsertSessionRunSkeleton(%q): %v", ticket.URL, err)
	}
	spawn := storepkg.Spawn{RunID: plan.RunID(runID), Pgid: 999, StartedAt: testAt, LogPath: "/state/runs/1.jsonl"}
	if err := store.RecordSpawn(t.Context(), spawn); err != nil {
		t.Fatalf("RecordSpawn(%d): %v", runID, err)
	}
	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	cfg.ClaudeProjectsDir = t.TempDir()
	project := filepath.Join(cfg.ClaudeProjectsDir, "-some-project")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatalf("MkdirAll(%q): %v", project, err)
	}
	if err := os.WriteFile(filepath.Join(project, sessionID+".jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(transcript): %v", err)
	}
	obs := plan.Observation{
		Worktrees: map[plan.BranchRef]string{branchRef("repo", "cc-1"): worktreePath}, PRs: map[plan.BranchRef]plan.PR{},
	}
	fake := &runnertest.Runner{}
	fake.Run(999)
	var rn runner.Runner = fake
	if wrap != nil {
		rn = wrap(fake)
	}
	return &steerFixture{
		store: store, fake: fake, ticket: ticket, sessionID: sessionID, repoPath: repoPath, runsDir: ws.RunsDir,
		loop: newLoop(store, observing(obs), fixedClock(testAt.Add(30*time.Second)), cfg, ws, rn),
	}
}

func (f *steerFixture) send(t *testing.T, text string) {
	t.Helper()
	err := f.store.QueueVerbIntent(t.Context(), storepkg.VerbRequest{
		Ticket: f.ticket.URL, Verb: plan.VerbFollowUp, Payload: text, At: testAt,
	})
	if err != nil {
		t.Fatalf("QueueVerbIntent(follow-up): %v", err)
	}
}

func (f *steerFixture) tick(t *testing.T) {
	t.Helper()
	if err := f.loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
}

func (f *steerFixture) pendingFollowUps(t *testing.T) int {
	t.Helper()
	pending, err := f.store.PendingVerbIntents(t.Context(), plan.VerbFollowUp)
	if err != nil {
		t.Fatalf("PendingVerbIntents(follow-up): %v", err)
	}
	return len(pending)
}

func TestSteer(t *testing.T) {
	t.Parallel()

	t.Run("a follow-up on a running ticket cancels it as continued and resumes in the same tick", func(t *testing.T) {
		t.Parallel()
		f := newSteerFixture(t, nil)
		f.send(t, "use sqlite instead")

		f.tick(t)

		if diff := cmp.Diff([]int{999}, f.fake.Canceled()); diff != "" {
			t.Errorf("canceled mismatch (-want +got):\n%s", diff)
		}
		spawns := f.fake.Spawns()
		if len(spawns) != 1 || !spawns[0].Resume || spawns[0].SessionID != f.sessionID {
			t.Fatalf("spawns = %+v, want one resume of session %q", spawns, f.sessionID)
		}
		assertPromptGolden(t, "testdata/prompt-steer-interrupt.golden.txt", spawns[0].Prompt, f.runsDir)
		if got := f.pendingFollowUps(t); got != 0 {
			t.Errorf("pending follow-ups = %d, want 0", got)
		}
		ids, err := f.store.RunIDsForTicket(t.Context(), f.ticket.URL)
		if err != nil {
			t.Fatalf("RunIDsForTicket: %v", err)
		}
		if len(ids) != 2 {
			t.Fatalf("RunIDsForTicket() = %v, want the interrupted run and its resume", ids)
		}
		first, err := f.store.RunDetails(t.Context(), plan.RunID(ids[0]))
		if err != nil {
			t.Fatalf("RunDetails(%d): %v", ids[0], err)
		}
		if first.Outcome != option.Some(plan.OutcomeContinued) {
			t.Errorf("interrupted run outcome = %v, want %q", first.Outcome, plan.OutcomeContinued)
		}
		if got := gittest.Run(t, f.repoPath, "ls-remote", "origin", "refs/heads/cc-1"); got != "" {
			t.Errorf("ls-remote origin cc-1 = %q, want nothing pushed", got)
		}
	})

	t.Run("a run that survives the cancel keeps the follow-up pending", func(t *testing.T) {
		t.Parallel()
		f := newSteerFixture(t, func(fake *runnertest.Runner) runner.Runner { return stubbornRunner{fake} })
		f.send(t, "use sqlite instead")

		f.tick(t)
		if got := len(f.fake.Spawns()); got != 0 {
			t.Fatalf("spawns while the run is alive = %d, want 0", got)
		}
		if got := f.pendingFollowUps(t); got != 1 {
			t.Fatalf("pending follow-ups = %d, want 1", got)
		}

		f.fake.Vanish(999)
		f.tick(t)

		if spawns := f.fake.Spawns(); len(spawns) != 1 || !spawns[0].Resume {
			t.Errorf("spawns = %+v, want one resume once the run is gone", spawns)
		}
		if got := f.pendingFollowUps(t); got != 0 {
			t.Errorf("pending follow-ups = %d, want 0", got)
		}
	})

	t.Run("two messages before a tick each resume in order", func(t *testing.T) {
		t.Parallel()
		f := newSteerFixture(t, nil)
		f.send(t, "first message")
		f.send(t, "second message")

		f.tick(t)
		if spawns := f.fake.Spawns(); len(spawns) != 1 || !strings.Contains(spawns[0].Prompt, "first message") {
			t.Fatalf("spawns = %+v, want only the first message resumed", spawns)
		}
		if got := f.pendingFollowUps(t); got != 1 {
			t.Fatalf("pending follow-ups = %d, want 1", got)
		}

		f.tick(t)
		spawns := f.fake.Spawns()
		if len(spawns) != 2 || !strings.Contains(spawns[1].Prompt, "second message") ||
			!spawns[1].Resume || spawns[1].SessionID != f.sessionID {
			t.Fatalf("spawns = %+v, want the second message resumed in session %q", spawns, f.sessionID)
		}
		if got := f.pendingFollowUps(t); got != 0 {
			t.Errorf("pending follow-ups = %d, want 0", got)
		}
	})
}
