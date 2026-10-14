package loop_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	spawn     runner.SpawnConfig
	sessionID string
	row       storepkg.RunDetails
}

func followUpAfterAgentRun(t *testing.T, tc followUpCase) followUpResult {
	t.Helper()
	root, repoPath := repoWithOrigin(t)
	worktreePath := cutWorktree(t, repoPath, "cc-1")

	store := openStore(t)
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTickets(t.Context(), []storepkg.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	agentRunID, sessionID, err := store.InsertSessionRunSkeleton(t.Context(), storepkg.RunSkeleton{
		Ticket: ticket.URL, Kind: "agent", PromptHash: "hash-1", Session: storepkg.RunSession{StartNew: tc.startSession},
	})
	if err != nil {
		t.Fatal(err)
	}
	if tc.startSession && sessionID == "" {
		t.Fatal("a new session run came back without a session id")
	}
	spawn := storepkg.Spawn{RunID: plan.RunID(agentRunID), Pgid: 999, StartedAt: at, LogPath: "/state/runs/1.jsonl"}
	if err := store.RecordSpawn(t.Context(), spawn); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordDisposition(t.Context(), storepkg.Disposition{
		RunID: plan.RunID(agentRunID), Outcome: plan.OutcomeFailed, ExitCode: option.None[int](),
		Cost: option.None[float64](), EndedAt: at,
	}); err != nil {
		t.Fatal(err)
	}

	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	cfg.ClaudeProjectsDir = t.TempDir()
	if tc.writeTranscript {
		project := filepath.Join(cfg.ClaudeProjectsDir, "-some-project")
		if err := os.MkdirAll(project, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(project, sessionID+".jsonl"), []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if err := store.QueueVerbIntentWithPayload(t.Context(), storepkg.VerbRequest{
		Ticket: ticket.URL, Verb: plan.VerbFollowUp, Payload: "now fix the other test", At: at,
	}); err != nil {
		t.Fatal(err)
	}
	obs := plan.Observation{
		Worktrees: map[plan.BranchRef]string{branchRef("repo", "cc-1"): worktreePath}, PRs: map[plan.BranchRef]plan.PR{},
	}
	observe := func(context.Context) (plan.Observation, error) { return obs, nil }
	fake := runner.NewFake()
	lp := newLoop(store, observe, fixedClock(at), cfg, ws, fake)
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(fake.Spawns) != 1 {
		t.Fatalf("spawns = %d, want 1", len(fake.Spawns))
	}

	latest, err := store.LatestRunsByTicket(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	row, err := store.RunDetails(t.Context(), latest[plan.TicketURL(ticket.URL)].ID)
	if err != nil {
		t.Fatal(err)
	}
	return followUpResult{
		spawn:     fake.Spawns[0],
		sessionID: sessionID,
		row:       row,
	}
}

func TestFollowUpResumesTheSessionWhenItsTranscriptExists(t *testing.T) {
	got := followUpAfterAgentRun(t, followUpCase{startSession: true, writeTranscript: true})

	if !got.spawn.Resume || got.spawn.SessionID != got.sessionID {
		t.Errorf("spawn = resume %v session %q, want a resume of %q", got.spawn.Resume, got.spawn.SessionID, got.sessionID)
	}
	if got.row.SessionID != got.sessionID {
		t.Errorf("run session_id = %q, want the resumed session %q", got.row.SessionID, got.sessionID)
	}
	if got.row.Message != "now fix the other test" {
		t.Errorf("run message = %q, want the owner's raw text", got.row.Message)
	}
	if !strings.Contains(got.spawn.Prompt, "now fix the other test") ||
		strings.Contains(got.spawn.Prompt, "cc/skills/follow-up/SKILL.md") {
		t.Errorf("prompt = %q, want the message wrapper and not the fresh follow-up skill", got.spawn.Prompt)
	}
}

func TestFollowUpSpawnsFreshWhenTheTranscriptIsMissing(t *testing.T) {
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
	if !strings.Contains(got.spawn.Prompt, "cc/skills/follow-up/SKILL.md") {
		t.Errorf("prompt = %q, want the fresh follow-up prompt", got.spawn.Prompt)
	}
}

func TestFollowUpSpawnsFreshWhenTheRunHasNoSession(t *testing.T) {
	got := followUpAfterAgentRun(t, followUpCase{})

	if got.spawn.Resume {
		t.Error("spawn resumes although the earlier run had no session")
	}
	if !strings.Contains(got.spawn.Prompt, "cc/skills/follow-up/SKILL.md") {
		t.Errorf("prompt = %q, want the fresh follow-up prompt", got.spawn.Prompt)
	}
}

func TestALaunchedAgentRunStartsASessionAndOtherKindsDoNot(t *testing.T) {
	f := newLoopFixture(t)
	f.AuthoriseAll(t)

	f.Tick(t)

	implements := f.ImplementSpawns()
	if len(implements) != 1 {
		t.Fatalf("implement spawns = %d, want 1", len(implements))
	}
	if implements[0].SessionID == "" || implements[0].Resume {
		t.Errorf("implement spawn = session %q resume %v, want a new session", implements[0].SessionID, implements[0].Resume)
	}
}
