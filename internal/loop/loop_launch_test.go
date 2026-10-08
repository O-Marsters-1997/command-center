package loop_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/cctest"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

func noOpObserve(context.Context) (plan.Observation, error) { return plan.Observation{}, nil }

func authoriseTicket(t *testing.T, store *storepkg.Store, ticketURL, hash string, at time.Time) {
	t.Helper()
	if err := store.QueueLaunchIntent(t.Context(), ticketURL, hash, "group-"+ticketURL, at); err != nil {
		t.Fatal(err)
	}
}

func TestLoopCutsAndSpawnsAnEligibleTicket(t *testing.T) {
	f := newLoopFixture(t)
	f.AuthoriseAll(t)
	f.Tick(t)

	if len(f.Fake.Spawns) != 1 {
		t.Fatalf("spawns = %d, want 1", len(f.Fake.Spawns))
	}
	spawned := f.Fake.Spawns[0]
	if !strings.HasSuffix(spawned.WorktreePath, "wt-cc-1") {
		t.Errorf("worktree path = %q, want it to end in wt-cc-1", spawned.WorktreePath)
	}
	if spawned.SettingsPath != f.WS.SettingsPath {
		t.Errorf("settings path = %q, want %q", spawned.SettingsPath, f.WS.SettingsPath)
	}
	if spawned.SystemPromptPath != f.WS.SystemPromptPath {
		t.Errorf("system prompt path = %q, want %q", spawned.SystemPromptPath, f.WS.SystemPromptPath)
	}
	if _, err := os.Stat(spawned.PromptPath); err != nil {
		t.Errorf("prompt file was not written: %v", err)
	}

	summary, ok := f.Latest(t)[f.Tickets[0].URL]
	if !ok {
		t.Fatal("no run recorded for sandbox://CC-1")
	}
	if summary.Pgid == nil || *summary.Pgid != 1 {
		t.Errorf("pgid = %v, want 1", summary.Pgid)
	}
	if summary.BaselineSHA == "" {
		t.Error("baseline_sha is empty")
	}
	if summary.HasOutcome {
		t.Error("a freshly spawned run must not already have an outcome")
	}
}

func TestLoopWritesTheComposedPromptAndTicketBody(t *testing.T) {
	ticket := sandboxTicket("1")
	ticket.Body = "fake ticket body"
	f := newLoopFixture(t, withTickets(ticket))
	f.AuthoriseAll(t)
	f.Tick(t)

	if len(f.Fake.Spawns) != 1 {
		t.Fatalf("spawns = %d, want 1", len(f.Fake.Spawns))
	}
	written, err := os.ReadFile(f.Fake.Spawns[0].PromptPath)
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, goldenPrompt, written)
}

func TestLoopNeverSpawnsOnAPromptHashMismatch(t *testing.T) {
	f := newLoopFixture(t)
	authoriseTicket(t, f.Store, f.Tickets[0].URL, plan.Hash("a prompt this ticket never composed to"), testAt)
	for range 3 {
		f.Tick(t)
	}

	if len(f.Fake.Spawns) != 0 {
		t.Errorf("spawns = %d, want 0: a hash the ticket no longer composes to must never be spawned",
			len(f.Fake.Spawns))
	}
	if _, ran := f.Latest(t)[f.Tickets[0].URL]; ran {
		t.Error("no run should ever be recorded for a ticket whose authorised hash no longer matches")
	}
}

func TestLoopRecordsCutFailedWithoutClaimingAPgid(t *testing.T) {
	f := newLoopFixture(t, withFailingTp())
	f.AuthoriseAll(t)
	f.Tick(t)

	if len(f.Fake.Spawns) != 0 {
		t.Errorf("spawns = %d, want 0: a cut failure must never reach Spawn", len(f.Fake.Spawns))
	}
	summary := f.Latest(t)[f.Tickets[0].URL]
	if !summary.HasOutcome || summary.Outcome != plan.OutcomeCutFailed {
		t.Errorf("summary = %+v, want cut_failed", summary)
	}
	if summary.Pgid != nil {
		t.Errorf("pgid = %v, want nil: a cut failure never claims a pgid", summary.Pgid)
	}
}

func TestLoopCapsLaunchesAtMaxAgentsMinusCurrentlyRunning(t *testing.T) {
	f := newLoopFixture(t, withTickets(sandboxTicket("1"), sandboxTicket("2")))
	f.AuthoriseAll(t)
	f.Tick(t)

	if len(f.Fake.Spawns) != 1 {
		t.Fatalf("spawns = %d, want exactly 1 (max_agents = 1)", len(f.Fake.Spawns))
	}
	latest := f.Latest(t)
	if _, ran := latest["sandbox://CC-1"]; !ran {
		t.Error("sandbox://CC-1 (first in ticket order) should have launched")
	}
	if _, ran := latest["sandbox://CC-2"]; ran {
		t.Error("sandbox://CC-2 should have stayed queued: no free slot")
	}
}

func TestLoopDisposesADeadRunByCommitsAfterItsOwnBaseline(t *testing.T) {
	_, repoPath := repoWithOrigin(t)
	worktreePath := filepath.Join(t.TempDir(), "wt")
	runGit(t, "-C", repoPath, "worktree", "add", "-b", "cc-1", worktreePath, "origin/main")
	baseline := strings.TrimSpace(runGitOutput(t, "-C", repoPath, "rev-parse", "refs/heads/cc-1"))

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

	obs := plan.Observation{Worktrees: map[string]string{plan.BranchKey("repo", "cc-1"): worktreePath}}
	observe := func(context.Context) (plan.Observation, error) { return obs, nil }

	fake := runner.NewFake()
	fake.CanReap[999] = true
	fake.ReapCode[999] = 0
	// alive defaults to false in the map (zero value), i.e. the run reads dead this tick.

	cfg, ws := testConfigAndWorkspace(t, filepath.Dir(repoPath), 0, nil)
	lp := loop.NewLoop(store, observe, fixedClock(at.Add(30*time.Second)), cfg, ws, fake)
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	latest, err := store.LatestRunsByTicket(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	summary := latest[ticket.URL]
	if !summary.HasOutcome || summary.Outcome != plan.OutcomeFailed {
		t.Fatalf("summary = %+v, want failed (no commits after baseline)", summary)
	}
	if summary.ExitCode == nil || *summary.ExitCode != 0 {
		t.Errorf("exit code = %v, want 0", summary.ExitCode)
	}

	// A second run against the same branch, with a real commit after ITS OWN baseline: must
	// derive push, and the first run's now-stale baseline must not leak into this one.
	runGit(t, "-C", worktreePath, "commit", "-q", "--allow-empty", "-m", "agent work")
	newBaseline := strings.TrimSpace(runGitOutput(t, "-C", repoPath, "rev-parse", "refs/heads/cc-1"))
	runGit(t, "-C", worktreePath, "commit", "-q", "--allow-empty", "-m", "more agent work")

	runID2, err := store.InsertRunSkeleton(t.Context(), ticket.URL, "agent", newBaseline, "hash-2")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordSpawn(t.Context(), runID2, 1000, at, "/state/runs/2.jsonl"); err != nil {
		t.Fatal(err)
	}
	fake.CanReap[1000] = true
	fake.ReapCode[1000] = 0

	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("second RunOnce: %v", err)
	}
	latest, err = store.LatestRunsByTicket(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	summary = latest[ticket.URL]
	if !summary.HasOutcome || summary.Outcome != plan.OutcomePush {
		t.Fatalf("summary = %+v, want push (one commit after its own baseline)", summary)
	}
}

func TestLoopDisposesAKilledRunWithUnsettledPartials(t *testing.T) {
	_, repoPath := repoWithOrigin(t)
	worktreePath := filepath.Join(t.TempDir(), "wt")
	runGit(t, "-C", repoPath, "worktree", "add", "-b", "cc-1", worktreePath, "origin/main")
	baseline := strings.TrimSpace(runGitOutput(t, "-C", repoPath, "rev-parse", "refs/heads/cc-1"))

	dsn := cctest.DSN(t)
	store := openStoreAt(t, dsn)
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTickets(t.Context(), []storepkg.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}

	runID, err := store.InsertRunSkeleton(t.Context(), ticket.URL, "agent", baseline, "hash-1")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	logPath := filepath.Join(t.TempDir(), "1.jsonl")
	partial := `{"type":"assistant","request_id":"r1","message":{"model":"claude-sonnet-5",` +
		`"usage":{"input_tokens":12,"output_tokens":8},"content":[]}}` + "\n"
	if err := os.WriteFile(logPath, []byte(partial), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordSpawn(t.Context(), runID, 999, at, logPath); err != nil {
		t.Fatal(err)
	}

	obs := plan.Observation{Worktrees: map[string]string{plan.BranchKey("repo", "cc-1"): worktreePath}}
	observe := func(context.Context) (plan.Observation, error) { return obs, nil }

	fake := runner.NewFake()
	fake.CanReap[999] = true
	fake.ReapCode[999] = 137 // killed

	cfg, ws := testConfigAndWorkspace(t, filepath.Dir(repoPath), 0, nil)
	lp := loop.NewLoop(store, observe, fixedClock(at.Add(30*time.Second)), cfg, ws, fake)

	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	row := readRunMetrics(t, dsn, runID)
	if row.TokensIn.Int64 != 12 || row.TokensOut.Int64 != 8 {
		t.Errorf("metrics = %+v, want the log's partial totals", row)
	}
	if !row.MetricsSettled.Valid || row.MetricsSettled.Bool {
		t.Errorf("metrics_settled = %+v, want false (a killed run's log never reached a result line)", row.MetricsSettled)
	}

	latest, err := store.LatestRunsByTicket(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if summary := latest[ticket.URL]; !summary.HasOutcome || summary.Outcome != plan.OutcomeFailed {
		t.Errorf("summary = %+v, want failed (no commits after baseline)", summary)
	}
}

func TestLoopDisposesARunAndRecordsItsUtilizationReadings(t *testing.T) {
	_, repoPath := repoWithOrigin(t)
	worktreePath := filepath.Join(t.TempDir(), "wt")
	runGit(t, "-C", repoPath, "worktree", "add", "-b", "cc-1", worktreePath, "origin/main")
	baseline := strings.TrimSpace(runGitOutput(t, "-C", repoPath, "rev-parse", "refs/heads/cc-1"))

	store := openStore(t)
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTickets(t.Context(), []storepkg.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}

	runID, err := store.InsertRunSkeleton(t.Context(), ticket.URL, "agent", baseline, "hash-1")
	if err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(t.TempDir(), "run.jsonl")
	line := `{"type":"rate_limit_event","rate_limit_info":{"unifiedWindows":{` +
		`"five_hour":{"utilization":0.05,"resetsAt":1787665200},` +
		`"seven_day":{"utilization":0.19,"resetsAt":1788159600}}}}` + "\n"
	if err := os.WriteFile(logPath, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	if err := store.RecordSpawn(t.Context(), runID, 999, at, logPath); err != nil {
		t.Fatal(err)
	}

	obs := plan.Observation{Worktrees: map[string]string{plan.BranchKey("repo", "cc-1"): worktreePath}}
	observe := func(context.Context) (plan.Observation, error) { return obs, nil }

	fake := runner.NewFake()
	fake.CanReap[999] = true
	fake.ReapCode[999] = 0

	cfg, ws := testConfigAndWorkspace(t, filepath.Dir(repoPath), 0, nil)
	lp := loop.NewLoop(store, observe, fixedClock(at.Add(30*time.Second)), cfg, ws, fake)
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	gauges, err := store.LatestReadings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got := gauges[agentlog.FiveHour]; got.Utilization != 0.05 {
		t.Errorf("five_hour gauge = %+v; want utilization 0.05", got)
	}
	if got := gauges[agentlog.SevenDay]; got.Utilization != 0.19 {
		t.Errorf("seven_day gauge = %+v; want utilization 0.19", got)
	}
}

func TestLoopDisposesADeadRunByOriginTipWhenTheWorktreeIsGone(t *testing.T) {
	_, repoPath := repoWithOrigin(t)
	worktreePath := filepath.Join(t.TempDir(), "wt")
	runGit(t, "-C", repoPath, "worktree", "add", "-b", "cc-1", worktreePath, "origin/main")
	baseline := strings.TrimSpace(runGitOutput(t, "-C", repoPath, "rev-parse", "refs/heads/cc-1"))

	// The agent's commit reaches origin (via its own push, or a prior tick's pushPushable)
	// before the worktree disappears -- issue #189's race.
	runGit(t, "-C", worktreePath, "commit", "-q", "--allow-empty", "-m", "agent work")
	runGit(t, "-C", worktreePath, "push", "-q", "origin", "cc-1")
	originTip := strings.TrimSpace(runGitOutput(t, "-C", repoPath, "rev-parse", "refs/remotes/origin/cc-1"))
	runGit(t, "-C", repoPath, "worktree", "remove", "--force", worktreePath)

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

	// No entry for "cc-1" in Worktrees: the worktree is gone by the time this tick disposes
	// the run. BranchTips is what observe would have read from origin/cc-1 this tick.
	obs := plan.Observation{BranchTips: map[string]string{plan.BranchKey("repo", "cc-1"): originTip}}
	observe := func(context.Context) (plan.Observation, error) { return obs, nil }

	fake := runner.NewFake()
	fake.CanReap[999] = true
	fake.ReapCode[999] = 0

	cfg, ws := testConfigAndWorkspace(t, filepath.Dir(repoPath), 0, nil)
	lp := loop.NewLoop(store, observe, fixedClock(at.Add(30*time.Second)), cfg, ws, fake)
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	latest, err := store.LatestRunsByTicket(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	summary := latest[ticket.URL]
	if !summary.HasOutcome || summary.Outcome != plan.OutcomePush {
		t.Fatalf("summary = %+v, want push: origin/cc-1 is ahead of baseline though the worktree is gone", summary)
	}
}

func TestLoopAppliesAKillIntentThenDisposesTheNowDeadRun(t *testing.T) {
	_, repoPath := repoWithOrigin(t)
	worktreePath := filepath.Join(t.TempDir(), "wt")
	runGit(t, "-C", repoPath, "worktree", "add", "-b", "cc-1", worktreePath, "origin/main")
	baseline := strings.TrimSpace(runGitOutput(t, "-C", repoPath, "rev-parse", "refs/heads/cc-1"))

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
	if err := store.RecordSpawn(t.Context(), runID, 4242, at, "/state/runs/1.jsonl"); err != nil {
		t.Fatal(err)
	}
	if err := store.QueueVerbIntent(t.Context(), ticket.URL, "kill", at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	obs := plan.Observation{Worktrees: map[string]string{plan.BranchKey("repo", "cc-1"): worktreePath}}
	observe := func(context.Context) (plan.Observation, error) { return obs, nil }

	fake := runner.NewFake()
	fake.Alive[4242] = true
	fake.CanReap[4242] = true
	fake.ReapCode[4242] = 143

	cfg, ws := testConfigAndWorkspace(t, filepath.Dir(repoPath), 0, nil)
	lp := loop.NewLoop(store, observe, fixedClock(at.Add(time.Minute)), cfg, ws, fake)
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if len(fake.Canceled) != 1 || fake.Canceled[0] != 4242 {
		t.Errorf("canceled = %v, want [4242]", fake.Canceled)
	}

	pending, err := store.PendingVerbIntents(t.Context(), "kill")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Errorf("pending kill intents = %+v, want none: consumed", pending)
	}

	latest, err := store.LatestRunsByTicket(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	summary := latest[ticket.URL]
	if !summary.HasOutcome {
		t.Fatal("the killed run was not disposed of in the same tick")
	}
	if summary.ExitCode == nil || *summary.ExitCode != 143 {
		t.Errorf("exit code = %v, want 143", summary.ExitCode)
	}
}

func TestLoopSpendLimit5h(t *testing.T) {
	tests := []struct {
		name         string
		utilizations []float64
		wantSpawns   int
	}{
		{"pauses at the limit", []float64{0.80}, 0},
		{"launches under the limit", []float64{0.50}, 1},
		{"resumes once a newer reading drops below the limit", []float64{0.85, 0.50}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newLoopFixture(t, withSpendLimit5h(80))
			f.AuthoriseAll(t)
			for i, utilization := range tt.utilizations {
				at := testAt.Add(time.Duration(i) * time.Second)
				reading := agentlog.Reading{
					Window: agentlog.FiveHour, Utilization: utilization,
					ResetsAt: testAt.Add(time.Duration(i+1) * time.Hour), At: at,
				}
				if err := f.Store.RecordReadings(t.Context(), []agentlog.Reading{reading}); err != nil {
					t.Fatal(err)
				}
				f.Tick(t)
			}
			if len(f.Fake.Spawns) != tt.wantSpawns {
				t.Errorf("spawns = %d, want %d", len(f.Fake.Spawns), tt.wantSpawns)
			}
		})
	}
}

func TestLoopSpendPauseNeverKillsALiveRun(t *testing.T) {
	_, repoPath := repoWithOrigin(t)
	worktreePath := filepath.Join(t.TempDir(), "wt")
	runGit(t, "-C", repoPath, "worktree", "add", "-b", "cc-1", worktreePath, "origin/main")
	baseline := strings.TrimSpace(runGitOutput(t, "-C", repoPath, "rev-parse", "refs/heads/cc-1"))

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
	if err := store.RecordSpawn(t.Context(), runID, 4242, at, "/state/runs/1.jsonl"); err != nil {
		t.Fatal(err)
	}
	reading := agentlog.Reading{Window: agentlog.FiveHour, Utilization: 0.95, ResetsAt: at.Add(time.Hour), At: at}
	if err := store.RecordReadings(t.Context(), []agentlog.Reading{reading}); err != nil {
		t.Fatal(err)
	}

	obs := plan.Observation{Worktrees: map[string]string{plan.BranchKey("repo", "cc-1"): worktreePath}}
	observe := func(context.Context) (plan.Observation, error) { return obs, nil }

	fake := runner.NewFake()
	fake.Alive[4242] = true

	cfg, ws := testConfigAndWorkspace(t, filepath.Dir(repoPath), 0, nil)
	cfg.SpendLimit5h = 80
	lp := loop.NewLoop(store, observe, fixedClock(at.Add(30*time.Second)), cfg, ws, fake)
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if len(fake.Canceled) != 0 {
		t.Errorf("canceled pgids = %v, want none: spend_limit_5h never kills a live run", fake.Canceled)
	}
	if !fake.Alive[4242] {
		t.Error("the live run's process was stopped; spend_limit_5h must leave it running")
	}
}

func runGit(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func runGitOutput(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Env = os.Environ()
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return string(out)
}
