package loop_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/cctest"

	"github.com/O-Marsters-1997/command-center/internal/option"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

type followUpCase struct {
	startSession    bool
	writeTranscript bool
}

type followUpResult struct {
	spawn           runner.SpawnConfig
	sessionID       string
	storedSessionID string
	row             storepkg.SessionRun
	runsDir         string
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

	at := testAt
	agentRunID, sessionID, err := store.InsertSessionRunSkeleton(t.Context(), storepkg.RunSkeleton{
		Ticket: ticket.URL, Kind: "agent", PromptHash: "hash-1", Session: storepkg.RunSession{StartNew: tc.startSession},
	})
	if err != nil {
		t.Fatalf("InsertSessionRunSkeleton(%q): %v", ticket.URL, err)
	}
	if tc.startSession && sessionID == "" {
		t.Fatal("a new session run came back without a session id")
	}
	spawn := storepkg.Spawn{RunID: plan.RunID(agentRunID), Pgid: 999, StartedAt: at, LogPath: "/state/runs/1.jsonl"}
	if err := store.RecordSpawn(t.Context(), spawn); err != nil {
		t.Fatalf("RecordSpawn(%d): %v", agentRunID, err)
	}
	if err := store.RecordDisposition(t.Context(), storepkg.Disposition{
		RunID: plan.RunID(agentRunID), Outcome: plan.OutcomeFailed, ExitCode: option.None[int](),
		Cost: option.None[float64](), EndedAt: at,
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
			t.Fatalf("WriteFile(%q): %v", sessionID+".jsonl", err)
		}
	}

	if err := store.QueueVerbIntentWithPayload(t.Context(), storepkg.VerbRequest{
		Ticket: ticket.URL, Verb: plan.VerbFollowUp, Payload: "now fix the other test", At: at,
	}); err != nil {
		t.Fatalf("QueueVerbIntentWithPayload(%q, follow-up): %v", ticket.URL, err)
	}
	obs := plan.NewObservation(testAt)
	obs.Worktrees[branchRef("repo", "cc-1")] = worktreePath
	observe := func(context.Context) (plan.Observation, error) { return obs, nil }
	fake := runner.NewFake()
	lp := newLoop(store, observe, cctest.Clock{At: at}, cfg, ws, fake)
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(fake.Spawns) != 1 {
		t.Fatalf("spawns = %d, want 1", len(fake.Spawns))
	}

	runs, err := store.SessionRunsForTicket(t.Context(), ticket.URL)
	if err != nil {
		t.Fatalf("SessionRunsForTicket(%q): %v", ticket.URL, err)
	}
	if len(runs) == 0 {
		t.Fatal("SessionRunsForTicket returned no follow-up")
	}
	latestSessionID, err := store.LatestSessionID(t.Context(), ticket.URL)
	if err != nil {
		t.Fatalf("LatestSessionID(%q): %v", ticket.URL, err)
	}
	return followUpResult{
		spawn:           fake.Spawns[0],
		sessionID:       sessionID,
		storedSessionID: latestSessionID,
		row:             runs[len(runs)-1],
		runsDir:         ws.RunsDir,
	}
}

func TestFollowUpResumesTheSessionWhenItsTranscriptExists(t *testing.T) {
	t.Parallel()
	got := followUpAfterAgentRun(t, followUpCase{startSession: true, writeTranscript: true})

	if !got.spawn.Resume || got.spawn.SessionID != got.sessionID {
		t.Errorf("spawn = resume %v session %q, want a resume of %q", got.spawn.Resume, got.spawn.SessionID, got.sessionID)
	}
	if got.storedSessionID != got.sessionID {
		t.Errorf("latest stored session = %q, want resumed session %q", got.storedSessionID, got.sessionID)
	}
	if got.row.Message != "now fix the other test" {
		t.Errorf("run message = %q, want the owner's raw text", got.row.Message)
	}
	assertPromptGolden(t, "testdata/prompt-follow-up-resume.golden.txt", got.spawn.Prompt, got.runsDir)
}

func TestFollowUpSpawnsFreshWhenTheTranscriptIsMissing(t *testing.T) {
	t.Parallel()
	got := followUpAfterAgentRun(t, followUpCase{startSession: true})

	if got.spawn.Resume {
		t.Error("spawn resumes a session whose transcript does not exist")
	}
	if got.spawn.SessionID == "" || got.spawn.SessionID == got.sessionID {
		t.Errorf("spawn session = %q, want a new session distinct from %q", got.spawn.SessionID, got.sessionID)
	}
	if got.row.Message != "now fix the other test" {
		t.Errorf("run message = %q, want the owner's raw text kept on a fresh follow-up too", got.row.Message)
	}
	assertPromptGolden(t, "testdata/prompt-follow-up-fresh.golden.txt", got.spawn.Prompt, got.runsDir)
}

func TestFollowUpSpawnsFreshWhenTheRunHasNoSession(t *testing.T) {
	t.Parallel()
	got := followUpAfterAgentRun(t, followUpCase{})

	if got.spawn.Resume {
		t.Error("spawn resumes although the earlier run had no session")
	}
	assertPromptGolden(t, "testdata/prompt-follow-up-fresh.golden.txt", got.spawn.Prompt, got.runsDir)
}

func TestALaunchedAgentRunStartsASessionAndOtherKindsDoNot(t *testing.T) {
	t.Parallel()
	f := newLoopFixture(t, fixture{})
	f.AuthoriseAll(t)

	f.Tick(t)

	implements := f.Fake.Spawns
	if len(implements) != 1 {
		t.Fatalf("implement spawns = %d, want 1", len(implements))
	}
	if implements[0].SessionID == "" || implements[0].Resume {
		t.Errorf("implement spawn = session %q resume %v, want a new session", implements[0].SessionID, implements[0].Resume)
	}
}
