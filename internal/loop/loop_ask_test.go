package loop_test

import (
	"context"
	"fmt"
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

func TestLoopDisposesARunThatWroteItsQuestionFileAsAskedEvenWithCommits(t *testing.T) {
	root, repoPath := repoWithOrigin(t)
	worktreePath := filepath.Join(t.TempDir(), "wt")
	runGit(t, "-C", repoPath, "worktree", "add", "-b", "cc-1", worktreePath, "origin/main")
	baseline := strings.TrimSpace(runGitOutput(t, "-C", repoPath, "rev-parse", "refs/heads/cc-1"))
	runGit(t, "-C", worktreePath, "commit", "-q", "--allow-empty", "-m", "agent work")

	store := openStore(t)
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTickets(t.Context(), []storepkg.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}
	runID, err := store.InsertRunSkeleton(t.Context(), ticket.URL, "agent", baseline, "hash-1")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	if err := store.RecordSpawn(t.Context(), runID, 999, at, "/state/runs/1.jsonl"); err != nil {
		t.Fatal(err)
	}

	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	long := "Which database?\n" + strings.Repeat("x", 5000)
	questionPath := filepath.Join(ws.RunsDir, fmt.Sprintf("%d.question", runID))
	if err := os.WriteFile(questionPath, []byte(long), 0o600); err != nil {
		t.Fatal(err)
	}

	obs := plan.Observation{Worktrees: map[string]string{plan.BranchKey("repo", "cc-1"): worktreePath}}
	observe := func(context.Context) (plan.Observation, error) { return obs, nil }
	fake := runner.NewFake()
	fake.CanReap[999] = true
	lp := newLoop(store, observe, fixedClock(at.Add(30*time.Second)), cfg, ws, fake)
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	latest, err := store.LatestRunsByTicket(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	summary := latest[ticket.URL]
	if summary.Outcome != option.Some(plan.OutcomeAsked) {
		t.Fatalf("summary = %+v, want asked", summary)
	}
	if !strings.HasPrefix(summary.Question, "Which database?\n") || len(summary.Question) != 4096 {
		t.Errorf("question has %d bytes, starts %q, want 4096 bytes truncated", len(summary.Question), summary.Question[:16])
	}
	if got := runGitOutput(t, "-C", repoPath, "ls-remote", "origin", "refs/heads/cc-1"); strings.TrimSpace(got) != "" {
		t.Errorf("origin carries cc-1 (%q), want nothing pushed", got)
	}
}
