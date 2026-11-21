package loop_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/adapter/git/gittest"
	"github.com/O-Marsters-1997/command-center/internal/adapter/runner"
	"github.com/O-Marsters-1997/command-center/internal/adapter/runner/runnertest"
	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
)

func noOpObserve(context.Context) (plan.Observation, error) { return plan.Observation{}, nil }

func authoriseTicket(t *testing.T, store *storepkg.Store, ticketURL, hash string, at time.Time) {
	t.Helper()
	launch := storepkg.LaunchRequest{Ticket: ticketURL, PromptHash: hash, Group: "group-" + ticketURL, At: at}
	if err := store.QueueLaunchIntent(t.Context(), launch); err != nil {
		t.Fatalf("QueueLaunchIntent(%q): %v", ticketURL, err)
	}
}

type runFixture struct {
	root, repoPath, worktreePath, baseline string
	store                                  *storepkg.Store
	ticket                                 storepkg.Ticket
	cfg                                    config.Config
	ws                                     config.Workspace
}

func newRunFixture(t *testing.T) *runFixture {
	t.Helper()
	root, repoPath := repoWithOrigin(t)
	worktreePath := filepath.Join(t.TempDir(), "wt")
	gittest.Run(t, repoPath, "worktree", "add", "-b", "cc-1", worktreePath, "origin/main")
	store := openStore(t)
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTickets(t.Context(), []storepkg.Ticket{ticket}); err != nil {
		t.Fatalf("UpsertTickets(%q): %v", ticket.URL, err)
	}
	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	return &runFixture{
		root: root, repoPath: repoPath, worktreePath: worktreePath,
		baseline: gittest.Run(t, repoPath, "rev-parse", "refs/heads/cc-1"),
		store:    store, ticket: ticket, cfg: cfg, ws: ws,
	}
}

func (f *runFixture) spawn(t *testing.T, pgid int, baseline, logPath string) int64 {
	t.Helper()
	runID, _, err := f.store.InsertSessionRunSkeleton(t.Context(), storepkg.RunSkeleton{
		Ticket: f.ticket.URL, Kind: "agent", BaselineSHA: baseline, PromptHash: "hash-1",
	})
	if err != nil {
		t.Fatalf("InsertRunSkeleton(%q): %v", f.ticket.URL, err)
	}
	spawn := storepkg.Spawn{RunID: plan.RunID(runID), Pgid: pgid, StartedAt: testAt, LogPath: logPath}
	if err := f.store.RecordSpawn(t.Context(), spawn); err != nil {
		t.Fatalf("RecordSpawn(%d): %v", runID, err)
	}
	return runID
}

func (f *runFixture) worktreeObs() plan.Observation {
	return plan.Observation{Worktrees: map[plan.BranchRef]string{branchRef("repo", "cc-1"): f.worktreePath}}
}

func (f *runFixture) tick(t *testing.T, obs plan.Observation, rn runner.Runner, opts ...loop.Option) {
	t.Helper()
	lp := newLoop(f.store, observing(obs), fixedClock(testAt.Add(30*time.Second)), f.cfg, f.ws, rn, opts...)
	runOnce(t, lp)
}

func (f *runFixture) latest(t *testing.T) plan.RunSummary {
	t.Helper()
	latest, err := f.store.LatestRunsByTicket(t.Context())
	if err != nil {
		t.Fatalf("LatestRunsByTicket(): %v", err)
	}
	return latest[plan.TicketURL(f.ticket.URL)]
}

func writeLog(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "run.jsonl")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
	return path
}

type failingSpawn struct{ *runnertest.Runner }

var errSpawn = errors.New("spawn failed")

func (failingSpawn) Spawn(context.Context, runner.SpawnConfig) (runner.SpawnResult, error) {
	return runner.SpawnResult{}, errSpawn
}

func TestLaunch(t *testing.T) {
	t.Parallel()

	t.Run("cuts and spawns an authorised ticket", func(t *testing.T) {
		t.Parallel()
		f := newLoopFixture(t)
		f.AuthoriseAll(t)
		f.Tick(t)

		spawns := f.Fake.Spawns()
		if len(spawns) != 1 {
			t.Fatalf("spawns = %d, want 1", len(spawns))
		}
		spawned := spawns[0]
		if got := gittest.Run(t, spawned.WorktreePath, "branch", "--show-current"); got != "cc-1" {
			t.Errorf("spawn worktree %q is on branch %q, want cc-1", spawned.WorktreePath, got)
		}
		want := [2]string{f.WS.SettingsPath, f.WS.SystemPromptPath}
		if got := [2]string{spawned.SettingsPath, spawned.SystemPromptPath}; got != want {
			t.Errorf("spawn settings and system prompt = %q, want %q", got, want)
		}
		if _, err := os.Stat(spawned.PromptPath); err != nil {
			t.Errorf("Stat(prompt file) error = %v, want it written", err)
		}
		summary, ok := f.Latest(t)[f.Tickets[0].URL]
		if !ok {
			t.Fatal("LatestRunsByTicket() has no run for sandbox://CC-1")
		}
		if summary.Pgid != option.Some(1) || summary.BaselineSHA == "" || summary.Outcome.IsSome() {
			t.Errorf("run = pgid %v, baseline %q, outcome %v, want pgid 1, a baseline and no outcome",
				summary.Pgid, summary.BaselineSHA, summary.Outcome)
		}
		wantHash := config.Workflows()[config.DefaultWorkflow].Hash
		if summary.Step != "implement" || summary.WorkflowHash != wantHash {
			t.Errorf("run step %q hash %q, want implement and %q", summary.Step, summary.WorkflowHash, wantHash)
		}
	})

	t.Run("starts a new session for the implement run", func(t *testing.T) {
		t.Parallel()
		f := newLoopFixture(t)
		f.AuthoriseAll(t)
		f.Tick(t)

		spawns := f.Fake.Spawns()
		if len(spawns) != 1 || spawns[0].SessionID == "" || spawns[0].Resume {
			t.Errorf("spawns = %+v, want one starting a new session", spawns)
		}
	})

	t.Run("writes the composed prompt with the ticket body", func(t *testing.T) {
		t.Parallel()
		ticket := sandboxTicket("1")
		ticket.Body = "fake ticket body"
		f := newLoopFixture(t, withTickets(ticket))
		f.AuthoriseAll(t)
		f.Tick(t)

		spawns := f.Fake.Spawns()
		if len(spawns) != 1 {
			t.Fatalf("spawns = %d, want 1", len(spawns))
		}
		written, err := os.ReadFile(spawns[0].PromptPath)
		if err != nil {
			t.Fatalf("ReadFile(prompt): %v", err)
		}
		assertPromptGolden(t, "testdata/prompt-implement.golden.txt", string(written), filepath.Dir(spawns[0].PromptPath))
	})

	t.Run("spawns with the launch's model, effort and context", func(t *testing.T) {
		t.Parallel()
		f := newLoopFixture(t)
		url := f.Tickets[0].URL
		const launchContext = "prefer table tests"
		choice := plan.AgentChoice{Model: "opus", Effort: option.Some(plan.EffortHigh)}
		launch := storepkg.LaunchRequest{
			Ticket: url, Group: "g", At: testAt, Context: launchContext, Choice: choice,
			PromptHash: plan.Hash(plan.Compose(plan.Ticket{URL: plan.TicketURL(url), LaunchContext: launchContext})),
		}
		if err := f.Store.QueueLaunchIntent(t.Context(), launch); err != nil {
			t.Fatalf("QueueLaunchIntent(%q): %v", url, err)
		}
		f.Tick(t)

		spawns := f.Fake.Spawns()
		if len(spawns) != 1 {
			t.Fatalf("spawns = %d, want 1", len(spawns))
		}
		if spawns[0].Model != choice.Model || spawns[0].Effort != choice.Effort {
			t.Errorf("spawn model and effort = %q, %v, want %q, %v",
				spawns[0].Model, spawns[0].Effort, choice.Model, choice.Effort)
		}
		if !strings.Contains(spawns[0].Prompt, launchContext) {
			t.Errorf("spawn prompt = %q, want it to carry the launch context", spawns[0].Prompt)
		}
	})

	t.Run("a launch stored without a choice spawns on the configured model", func(t *testing.T) {
		t.Parallel()
		f := newLoopFixture(t)
		f.AuthoriseAll(t)
		f.Tick(t)

		spawns := f.Fake.Spawns()
		if len(spawns) != 1 {
			t.Fatalf("spawns = %d, want 1", len(spawns))
		}
		if spawns[0].Model != config.AgentModel || spawns[0].Effort.IsSome() {
			t.Errorf("spawn model and effort = %q, %v, want %q and none",
				spawns[0].Model, spawns[0].Effort, config.AgentModel)
		}
	})

	t.Run("acts on a ticket whatever the views scope out", func(t *testing.T) {
		t.Parallel()
		hidden := storepkg.Ticket{URL: "sandbox://HIDDEN", Repo: "other", Branch: "hidden"}
		f := newLoopFixture(t, withMaxAgents(2), withExtraRepo("other"), withTickets(hidden))
		authoriseTicket(t, f.Store, hidden.URL, composeHash(hidden.URL), testAt)
		f.Tick(t)

		if got := len(f.Fake.Spawns()); got != 1 {
			t.Errorf("spawns = %d, want 1", got)
		}
	})

	t.Run("never spawns on a prompt hash the ticket no longer composes to", func(t *testing.T) {
		t.Parallel()
		f := newLoopFixture(t)
		authoriseTicket(t, f.Store, f.Tickets[0].URL, plan.Hash("a prompt this ticket never composed to"), testAt)
		for range 3 {
			f.Tick(t)
		}

		if got := len(f.Fake.Spawns()); got != 0 {
			t.Errorf("spawns = %d, want 0", got)
		}
		if _, ran := f.Latest(t)[f.Tickets[0].URL]; ran {
			t.Error("LatestRunsByTicket() has a run, want none")
		}
	})

	t.Run("a failed cut is recorded with its cause and claims no pgid", func(t *testing.T) {
		t.Parallel()
		f := newLoopFixture(t, withFailingCut())
		f.AuthoriseAll(t)
		f.Tick(t)

		if got := len(f.Fake.Spawns()); got != 0 {
			t.Errorf("spawns = %d, want 0", got)
		}
		summary := f.Latest(t)[f.Tickets[0].URL]
		if summary.Outcome != option.Some(plan.OutcomeCutFailed) || summary.Pgid.IsSome() {
			t.Errorf("run = outcome %v, pgid %v, want cut_failed and no pgid", summary.Outcome, summary.Pgid)
		}
		if !hasEvent(storetest.Events(t, f.Store, ""), storepkg.EventCutFailed, fmt.Sprint(errForge)) {
			t.Errorf("Events() has no %s event naming %q", storepkg.EventCutFailed, errForge)
		}
	})

	t.Run("a failed post hook still spawns and records no cut failure", func(t *testing.T) {
		t.Parallel()
		f := newLoopFixture(t)
		lp := newLoop(f.Store, noOpObserve, fixedClock(testAt), f.Cfg, f.WS, f.Fake,
			loop.WithForge(f.Forge), loop.WithWorktrees(postHookWorktrees{}))
		f.AuthoriseAll(t)
		runOnce(t, lp)

		if got := len(f.Fake.Spawns()); got != 1 {
			t.Errorf("spawns = %d, want 1", got)
		}
		if hasEvent(storetest.Events(t, f.Store, ""), storepkg.EventCutFailed, "") {
			t.Errorf("Events() has a %s event, want none", storepkg.EventCutFailed)
		}
	})

	t.Run("a failed spawn is recorded with its cause", func(t *testing.T) {
		t.Parallel()
		f := newLoopFixture(t)
		lp := newLoop(f.Store, noOpObserve, fixedClock(testAt), f.Cfg, f.WS, failingSpawn{f.Fake},
			loop.WithForge(f.Forge))
		f.AuthoriseAll(t)
		runOnce(t, lp)

		if !hasEvent(storetest.Events(t, f.Store, ""), storepkg.EventSpawnFailed, fmt.Sprint(errSpawn)) {
			t.Errorf("Events() has no %s event naming %q", storepkg.EventSpawnFailed, errSpawn)
		}
	})

	t.Run("pauses at the five-hour spend limit and resumes once a newer reading drops", func(t *testing.T) {
		t.Parallel()
		f := newLoopFixture(t, withSpendLimit5h(80))
		f.AuthoriseAll(t)
		var got []int
		for i, utilization := range []float64{0.85, 0.50} {
			reading := agentlog.Reading{
				Window: agentlog.FiveHour, Utilization: utilization,
				ResetsAt: testAt.Add(time.Duration(i+1) * time.Hour), At: testAt.Add(time.Duration(i) * time.Second),
			}
			if err := f.Store.RecordReadings(t.Context(), []agentlog.Reading{reading}); err != nil {
				t.Fatalf("RecordReadings: %v", err)
			}
			f.Tick(t)
			got = append(got, len(f.Fake.Spawns()))
		}
		if diff := cmp.Diff([]int{0, 1}, got); diff != "" {
			t.Errorf("spawns after each tick mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("the spend pause leaves a live run running", func(t *testing.T) {
		t.Parallel()
		f := newRunFixture(t)
		f.spawn(t, 4242, f.baseline, "/state/runs/1.jsonl")
		reading := agentlog.Reading{Window: agentlog.FiveHour, Utilization: 0.95, ResetsAt: testAt.Add(time.Hour), At: testAt}
		if err := f.store.RecordReadings(t.Context(), []agentlog.Reading{reading}); err != nil {
			t.Fatalf("RecordReadings: %v", err)
		}
		f.cfg.SpendLimit5h = 80
		fake := &runnertest.Runner{}
		fake.Run(4242)

		f.tick(t, f.worktreeObs(), fake)

		if got := fake.Canceled(); len(got) != 0 || !fake.Alive(4242) {
			t.Errorf("canceled = %v, alive = %v, want nothing cancelled and 4242 alive", got, fake.Alive(4242))
		}
	})
}
