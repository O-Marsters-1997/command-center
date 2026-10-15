package loop_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/option"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
	"github.com/O-Marsters-1997/command-center/internal/runner/runnertest"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

type followUpCase struct {
	startSession    bool
	writeTranscript bool
}

type followUpResult struct {
	spawn     runner.SpawnConfig
	sessionID string
	row       storepkg.RunDetails
	runsDir   string
}

func followUpAfterAgentRun(t *testing.T, tc followUpCase) followUpResult {
	t.Helper()
	root, repoPath := repoWithOrigin(t)
	worktreePath := cutWorktree(t, repoPath, "cc-1")
	store := openStore(t)
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTickets(t.Context(), []storepkg.Ticket{ticket}); err != nil {
		t.Fatalf("UpsertTickets(%q): %v", ticket.URL, err)
	}
	agentRunID, sessionID, err := store.InsertSessionRunSkeleton(t.Context(), storepkg.RunSkeleton{
		Ticket: ticket.URL, Kind: "agent", PromptHash: "hash-1", Session: storepkg.RunSession{StartNew: tc.startSession},
	})
	if err != nil {
		t.Fatalf("InsertSessionRunSkeleton(%q): %v", ticket.URL, err)
	}
	spawn := storepkg.Spawn{RunID: plan.RunID(agentRunID), Pgid: 999, StartedAt: testAt, LogPath: "/state/runs/1.jsonl"}
	if err := store.RecordSpawn(t.Context(), spawn); err != nil {
		t.Fatalf("RecordSpawn(%d): %v", agentRunID, err)
	}
	if err := store.RecordDisposition(t.Context(), storepkg.Disposition{
		RunID: plan.RunID(agentRunID), Outcome: plan.OutcomeFailed, ExitCode: option.None[int](),
		Cost: option.None[float64](), EndedAt: testAt,
	}); err != nil {
		t.Fatalf("RecordDisposition(%d): %v", agentRunID, err)
	}
	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	cfg.ClaudeProjectsDir = t.TempDir()
	if tc.writeTranscript {
		project := filepath.Join(cfg.ClaudeProjectsDir, "-some-project")
		if err := os.MkdirAll(project, 0o700); err != nil {
			t.Fatalf("MkdirAll(%q): %v", project, err)
		}
		if err := os.WriteFile(filepath.Join(project, sessionID+".jsonl"), []byte("{}\n"), 0o600); err != nil {
			t.Fatalf("WriteFile(transcript): %v", err)
		}
	}
	if err := store.QueueVerbIntentWithPayload(t.Context(), storepkg.VerbRequest{
		Ticket: ticket.URL, Verb: plan.VerbFollowUp, Payload: "now fix the other test", At: testAt,
	}); err != nil {
		t.Fatalf("QueueVerbIntentWithPayload(follow-up): %v", err)
	}
	obs := plan.Observation{
		Worktrees: map[plan.BranchRef]string{branchRef("repo", "cc-1"): worktreePath}, PRs: map[plan.BranchRef]plan.PR{},
	}
	fake := &runnertest.Runner{}
	lp := newLoop(store, observing(obs), fixedClock(testAt), cfg, ws, fake)
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	spawns := fake.Spawns()
	if len(spawns) != 1 {
		t.Fatalf("spawns = %d, want 1", len(spawns))
	}
	latest, err := store.LatestRunsByTicket(t.Context())
	if err != nil {
		t.Fatalf("LatestRunsByTicket(): %v", err)
	}
	row, err := store.RunDetails(t.Context(), latest[plan.TicketURL(ticket.URL)].ID)
	if err != nil {
		t.Fatalf("RunDetails(%q): %v", ticket.URL, err)
	}
	return followUpResult{spawn: spawns[0], sessionID: sessionID, row: row, runsDir: ws.RunsDir}
}

func TestFollowUp(t *testing.T) {
	t.Parallel()

	t.Run("spawns a fresh run in the existing worktree with the typed prompt", func(t *testing.T) {
		t.Parallel()
		f := newPushFixture(t)
		if err := f.Store.QueueVerbIntentWithPayload(t.Context(), storepkg.VerbRequest{
			Ticket: f.Ticket.URL, Verb: plan.VerbFollowUp, Payload: "now fix the other test", At: testAt,
		}); err != nil {
			t.Fatalf("QueueVerbIntentWithPayload: %v", err)
		}
		fake := &runnertest.Runner{}
		f.reset(testAt, fake)
		f.tick(t)

		spawns := fake.Spawns()
		if len(spawns) != 1 {
			t.Fatalf("spawns = %d, want 1", len(spawns))
		}
		spawned := spawns[0]
		if spawned.WorktreePath != f.Worktree || spawned.SystemPromptPath != "" {
			t.Errorf("spawn = worktree %q, system prompt %q, want %q and none", spawned.WorktreePath,
				spawned.SystemPromptPath, f.Worktree)
		}
		assertPromptGolden(t, "testdata/prompt-follow-up-fresh.golden.txt", spawned.Prompt, f.ws.RunsDir)
		latest, err := f.Store.LatestRunsByTicket(t.Context())
		if err != nil {
			t.Fatalf("LatestRunsByTicket: %v", err)
		}
		if kind := latest[plan.TicketURL(f.Ticket.URL)].Kind; kind != "follow_up" {
			t.Errorf("run kind = %q, want follow_up", kind)
		}
		if onDisk := readFile(t, filepath.Join(f.ws.RunsDir, "1.prompt")); string(onDisk) != spawned.Prompt {
			t.Errorf("stored prompt = %q, want the spawned prompt %q", onDisk, spawned.Prompt)
		}
		pending, err := f.Store.PendingVerbIntents(t.Context(), plan.VerbFollowUp)
		if err != nil {
			t.Fatalf("PendingVerbIntents: %v", err)
		}
		if len(pending) != 0 {
			t.Errorf("PendingVerbIntents(follow-up) = %+v, want none", pending)
		}
	})

	t.Run("refuses with no worktree", func(t *testing.T) {
		t.Parallel()
		f := newPushFixture(t)
		delete(f.Obs.Worktrees, branchRef("repo", "cc-1"))
		if err := f.Store.QueueVerbIntentWithPayload(t.Context(), storepkg.VerbRequest{
			Ticket: f.Ticket.URL, Verb: plan.VerbFollowUp, Payload: "do the thing", At: testAt,
		}); err != nil {
			t.Fatalf("QueueVerbIntentWithPayload: %v", err)
		}
		fake := &runnertest.Runner{}
		f.reset(testAt, fake)
		f.tick(t)

		if got := len(fake.Spawns()); got != 0 {
			t.Fatalf("spawns = %d, want 0", got)
		}
		events := f.events(t, "follow_up_refused")
		if len(events) != 1 || events[0].TicketURL != f.Ticket.URL || !strings.Contains(events[0].Detail, "no worktree") {
			t.Errorf("follow_up_refused events = %+v, want one naming the missing worktree", events)
		}
	})

	t.Run("resumes the session when its transcript exists", func(t *testing.T) {
		t.Parallel()
		got := followUpAfterAgentRun(t, followUpCase{startSession: true, writeTranscript: true})

		if !got.spawn.Resume || got.spawn.SessionID != got.sessionID || got.row.SessionID != got.sessionID {
			t.Errorf("spawn = resume %v session %q, row session %q, want a resume of %q",
				got.spawn.Resume, got.spawn.SessionID, got.row.SessionID, got.sessionID)
		}
		if got.row.Message != "now fix the other test" {
			t.Errorf("run message = %q, want the owner's raw text", got.row.Message)
		}
		assertPromptGolden(t, "testdata/prompt-follow-up-resume.golden.txt", got.spawn.Prompt, got.runsDir)
	})

	t.Run("spawns fresh when the transcript is missing", func(t *testing.T) {
		t.Parallel()
		got := followUpAfterAgentRun(t, followUpCase{startSession: true})

		if got.spawn.Resume || got.spawn.SessionID == "" || got.spawn.SessionID == got.sessionID {
			t.Errorf("spawn = resume %v session %q, want a new session distinct from %q",
				got.spawn.Resume, got.spawn.SessionID, got.sessionID)
		}
		if got.row.Message != "now fix the other test" {
			t.Errorf("run message = %q, want the owner's raw text", got.row.Message)
		}
		assertPromptGolden(t, "testdata/prompt-follow-up-fresh.golden.txt", got.spawn.Prompt, got.runsDir)
	})

	t.Run("spawns fresh when the run had no session", func(t *testing.T) {
		t.Parallel()
		got := followUpAfterAgentRun(t, followUpCase{})

		if got.spawn.Resume {
			t.Error("spawn resumes, want a fresh run")
		}
		assertPromptGolden(t, "testdata/prompt-follow-up-fresh.golden.txt", got.spawn.Prompt, got.runsDir)
	})
}
