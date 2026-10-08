package loop_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

type reviewFixture struct {
	store    *storepkg.Store
	loop     *loop.Loop
	fake     *runner.Fake
	forge    *fakeForge
	runsDir  string
	worktree string
	ticket   storepkg.Ticket
	obs      *plan.Observation
}

// newReviewFixture is a ticket whose last run, of the given kind, pushed its tip to an open PR.
func newReviewFixture(t *testing.T, lastKind string) *reviewFixture {
	t.Helper()
	root, repoPath := repoWithOrigin(t)
	worktree := cutWorktree(t, repoPath, "cc-1")
	commitFile(t, worktree, "a.txt", "a\n")
	tip := strings.TrimSpace(runGitOutput(t, "-C", worktree, "rev-parse", "HEAD"))

	store := openStore(t)
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTickets(t.Context(), []storepkg.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"agent", "follow_up"} {
		runID, err := store.InsertRunSkeleton(t.Context(), ticket.URL, kind, "", "")
		if err != nil {
			t.Fatal(err)
		}
		if err := store.RecordDisposition(t.Context(), runID, plan.OutcomePush, nil, testAt, nil); err != nil {
			t.Fatal(err)
		}
		if kind == lastKind {
			break
		}
	}
	if err := store.RecordPush(t.Context(), ticket.URL, tip, "main", "base", testAt); err != nil {
		t.Fatal(err)
	}

	key := plan.BranchKey("repo", "cc-1")
	obs := plan.Observation{
		Worktrees: map[string]string{key: worktree},
		LocalTips: map[string]string{key: tip},
		PRs:       map[string]plan.PR{key: {Number: 1, State: plan.Open}},
	}
	observe := func(context.Context) (plan.Observation, error) { return obs, nil }
	shared := &obs

	fake := runner.NewFake()
	cfg, ws := testConfigAndWorkspace(t, root, 2, []string{"agent", "--max-turns", "60"})
	cfg.ReviewAgentCommand = []string{"agent", "--max-turns", "20"}
	forge := &fakeForge{t: t}
	lp := loop.NewLoop(store, observe, fixedClock(testAt), cfg, ws, fake)
	lp.SetForge(forge)
	return &reviewFixture{
		store: store, loop: lp, fake: fake, forge: forge, runsDir: ws.RunsDir,
		worktree: worktree, ticket: ticket, obs: shared,
	}
}

func (f *reviewFixture) tick(t *testing.T) {
	t.Helper()
	if err := f.loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
}

func (f *reviewFixture) latestRun(t *testing.T) plan.RunSummary {
	t.Helper()
	latest, err := f.store.LatestRunsByTicket(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return latest[f.ticket.URL]
}

func (f *reviewFixture) finishReview(t *testing.T, findings string) {
	t.Helper()
	run := f.latestRun(t)
	if findings != "" {
		path := filepath.Join(f.runsDir, strconv.FormatInt(run.ID, 10)+".findings.md")
		if err := os.WriteFile(path, []byte(findings), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	f.fake.Alive[*run.Pgid] = false
}

func TestAPushedImplementRunIsFollowedByExactlyOneReviewRun(t *testing.T) {
	f := newReviewFixture(t, "agent")

	f.tick(t)
	f.tick(t)

	if len(f.fake.Spawns) != 1 {
		t.Fatalf("spawns = %d, want 1 review", len(f.fake.Spawns))
	}
	spawn := f.fake.Spawns[0]
	if spawn.WorktreePath != f.worktree {
		t.Errorf("review worktree = %q, want the ticket's %q", spawn.WorktreePath, f.worktree)
	}
	if !strings.HasPrefix(spawn.Prompt, "/code-review --fix") || !strings.Contains(spawn.Prompt, ".findings.md") {
		t.Errorf("prompt = %q, want the review command naming a findings file", spawn.Prompt)
	}
	if spawn.SystemPromptPath == "" && spawn.AgentsPath == "" && spawn.SettingsPath == "" {
		t.Errorf("review spawned with no agent files")
	}
	if got := f.latestRun(t).Kind; got != "review" {
		t.Errorf("latest run kind = %q, want review", got)
	}
}

func TestReviewSpawnsWithItsOwnLowerMaxTurns(t *testing.T) {
	f := newReviewFixture(t, "agent")

	f.tick(t)

	got := f.fake.Spawns[0].AgentCommand
	if !slices.Equal(got, []string{"agent", "--max-turns", "20"}) {
		t.Errorf("review agent command = %q, want the review cap of 20", got)
	}
}

func TestACleanReviewQueuesNothingAndLeavesTheTicketPushed(t *testing.T) {
	f := newReviewFixture(t, "agent")
	f.tick(t)

	f.finishReview(t, "")
	f.tick(t)
	f.tick(t)

	if len(f.fake.Spawns) != 1 {
		t.Errorf("spawns = %d, want only the review", len(f.fake.Spawns))
	}
	if run := f.latestRun(t); !run.HasOutcome || run.Outcome != plan.OutcomePush {
		t.Errorf("review outcome = %v (has=%v), want push so the ticket is not failed", run.Outcome, run.HasOutcome)
	}
	if len(f.forge.comments) != 0 {
		t.Errorf("comments = %q, want none", f.forge.comments)
	}
}

func TestReviewFindingsQueueAFollowUpCarryingThem(t *testing.T) {
	f := newReviewFixture(t, "agent")
	f.tick(t)

	f.finishReview(t, "## Move the cache to package store\n")
	f.tick(t)

	if len(f.fake.Spawns) != 2 {
		t.Fatalf("spawns = %d, want review then follow-up", len(f.fake.Spawns))
	}
	followUp := f.fake.Spawns[1]
	if !strings.Contains(followUp.Prompt, "cc/skills/follow-up/SKILL.md") ||
		!strings.Contains(followUp.Prompt, "Move the cache to package store") {
		t.Errorf("follow-up prompt = %q, want the follow-up skill with the findings", followUp.Prompt)
	}
	if got := f.latestRun(t).Kind; got != "follow_up" {
		t.Errorf("latest run kind = %q, want follow_up", got)
	}
}

func TestAReviewOfAFollowUpNeverQueuesASecondFollowUp(t *testing.T) {
	f := newReviewFixture(t, "follow_up")
	f.tick(t)
	if len(f.fake.Spawns) != 1 {
		t.Fatalf("spawns = %d, want the review of the follow-up", len(f.fake.Spawns))
	}

	f.finishReview(t, "## Rename Foo\n")
	f.tick(t)
	f.tick(t)

	if len(f.fake.Spawns) != 1 {
		t.Errorf("spawns = %d, want no second follow-up", len(f.fake.Spawns))
	}
	if len(f.forge.comments) != 1 || !strings.Contains(f.forge.comments[0], "Rename Foo") {
		t.Errorf("comments = %q, want the findings posted once to the PR", f.forge.comments)
	}
}

func TestNoReviewWithoutAnOpenPR(t *testing.T) {
	f := newReviewFixture(t, "agent")
	f.obs.PRs = map[string]plan.PR{}

	f.tick(t)

	if len(f.fake.Spawns) != 0 {
		t.Errorf("spawns = %d, want none while no PR is open", len(f.fake.Spawns))
	}
}
