package loop_test

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/adapter/git/gittest"
	"github.com/O-Marsters-1997/command-center/internal/adapter/runner/runnertest"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
)

const (
	killedRunAssistantLine = `{"type":"assistant","request_id":"r1","message":{"model":"claude-sonnet-5",` +
		`"usage":{"input_tokens":12,"output_tokens":8},"content":[]}}` + "\n"
	killedRunResultLine = `{"type":"result","total_cost_usd":0.42}` + "\n"
)

func TestDispose(t *testing.T) {
	t.Parallel()

	t.Run("judges a dead run by commits after its own baseline", func(t *testing.T) {
		t.Parallel()
		f := newRunFixture(t)
		f.spawn(t, 999, f.baseline, "/state/runs/1.jsonl")
		fake := &runnertest.Runner{}
		fake.Exit(999, 0)
		f.tick(t, f.worktreeObs(), fake)
		if got := f.latest(t); got.Outcome != option.Some(plan.OutcomeFailed) || got.ExitCode != option.Some(0) {
			t.Fatalf("first run = outcome %v, exit %v, want failed with exit 0", got.Outcome, got.ExitCode)
		}

		gittest.Run(t, f.worktreePath, "commit", "-q", "--allow-empty", "-m", "agent work")
		newBaseline := gittest.Run(t, f.repoPath, "rev-parse", "refs/heads/cc-1")
		gittest.Run(t, f.worktreePath, "commit", "-q", "--allow-empty", "-m", "more agent work")
		f.spawn(t, 1000, newBaseline, "/state/runs/2.jsonl")
		fake.Exit(1000, 0)
		f.tick(t, f.worktreeObs(), fake)

		if got := f.latest(t).Outcome; got != option.Some(plan.OutcomePush) {
			t.Errorf("second run outcome = %v, want push", got)
		}
	})

	t.Run("judges a dead run by the origin tip when the worktree is gone", func(t *testing.T) {
		t.Parallel()
		f := newRunFixture(t)
		gittest.Run(t, f.worktreePath, "commit", "-q", "--allow-empty", "-m", "agent work")
		gittest.Run(t, f.worktreePath, "push", "-q", "origin", "cc-1")
		originTip := gittest.Run(t, f.repoPath, "rev-parse", "refs/remotes/origin/cc-1")
		gittest.Run(t, f.repoPath, "worktree", "remove", "--force", f.worktreePath)
		f.spawn(t, 999, f.baseline, "/state/runs/1.jsonl")
		fake := &runnertest.Runner{}
		fake.Exit(999, 0)

		obs := plan.Observation{BranchTips: map[plan.BranchRef]plan.SHA{branchRef("repo", "cc-1"): plan.SHA(originTip)}}
		f.tick(t, obs, fake)

		if got := f.latest(t).Outcome; got != option.Some(plan.OutcomePush) {
			t.Errorf("outcome = %v, want push", got)
		}
	})

	t.Run("keeps the run's final message after disposal", func(t *testing.T) {
		t.Parallel()
		f := newRunFixture(t)
		f.spawn(t, 999, f.baseline, writeLog(t, killedRunAssistantLine+
			`{"type":"result","result":"Opened the PR.","total_cost_usd":0.42}`+"\n"))
		fake := &runnertest.Runner{}
		fake.Exit(999, 0)
		f.tick(t, f.worktreeObs(), fake)

		activities, err := f.store.Activities(t.Context())
		if err != nil {
			t.Fatalf("Activities: %v", err)
		}
		if len(activities) != 1 || activities[0].LastMessage != "Opened the PR." {
			t.Errorf("Activities() = %+v, want one ticket with last message %q", activities, "Opened the PR.")
		}
	})

	t.Run("records the run's utilization readings", func(t *testing.T) {
		t.Parallel()
		f := newRunFixture(t)
		line := `{"type":"rate_limit_event","rate_limit_info":{"unifiedWindows":{` +
			`"five_hour":{"utilization":0.05,"resetsAt":1787665200},` +
			`"seven_day":{"utilization":0.19,"resetsAt":1788159600}}}}` + "\n"
		f.spawn(t, 999, f.baseline, writeLog(t, line))
		fake := &runnertest.Runner{}
		fake.Exit(999, 0)
		f.tick(t, f.worktreeObs(), fake)

		gauges, err := f.store.LatestReadings(t.Context())
		if err != nil {
			t.Fatalf("LatestReadings(): %v", err)
		}
		got := map[agentlog.Window]float64{
			agentlog.FiveHour: gauges[agentlog.FiveHour].Utilization,
			agentlog.SevenDay: gauges[agentlog.SevenDay].Utilization,
		}
		want := map[agentlog.Window]float64{agentlog.FiveHour: 0.05, agentlog.SevenDay: 0.19}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("LatestReadings() utilization mismatch (-want +got):\n%s", diff)
		}
	})

	disposeKilledRun := func(
		t *testing.T, log string,
	) (cost map[agentlog.Window]float64, readings map[agentlog.Window]agentlog.Reading) {
		t.Helper()
		f := newRunFixture(t)
		f.spawn(t, 999, f.baseline, writeLog(t, log))
		fake := &runnertest.Runner{}
		fake.Exit(999, 137)
		f.tick(t, f.worktreeObs(), fake)

		if got := f.latest(t).Outcome; got != option.Some(plan.OutcomeFailed) {
			t.Errorf("outcome = %v, want failed", got)
		}
		cost, err := f.store.CCCostUSD(t.Context(), testAt.Add(time.Hour))
		if err != nil {
			t.Fatalf("CCCostUSD: %v", err)
		}
		readings, err = f.store.LatestReadingsFull(t.Context())
		if err != nil {
			t.Fatalf("LatestReadingsFull: %v", err)
		}
		return cost, readings
	}

	t.Run("a killed run with an unsettled log records no cost", func(t *testing.T) {
		t.Parallel()
		cost, readings := disposeKilledRun(t, killedRunAssistantLine+
			`{"type":"rate_limit_event","rate_limit_info":{"unifiedWindows":{`+
			`"five_hour":{"utilization":0.05,"resetsAt":1787665200}}}}`+"\n"+`{"type":"result","total_cost_usd":`)

		if cost[agentlog.FiveHour] != 0 {
			t.Errorf("CCCostUSD()[five_hour] = %v, want 0", cost[agentlog.FiveHour])
		}
		if got := readings[agentlog.FiveHour].Utilization; got != 0.05 {
			t.Errorf("LatestReadingsFull()[five_hour].Utilization = %v, want 0.05", got)
		}
	})

	t.Run("a killed run whose log settled records its cost", func(t *testing.T) {
		t.Parallel()
		cost, _ := disposeKilledRun(t, killedRunAssistantLine+killedRunResultLine)

		if cost[agentlog.FiveHour] != 0.42 {
			t.Errorf("CCCostUSD()[five_hour] = %v, want 0.42", cost[agentlog.FiveHour])
		}
	})
}
