//go:build demo

package demo_test

import (
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/demo"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

const scenarioDir = "../../demo/scenarios"

func play(t *testing.T, path string) *demo.Sim {
	t.Helper()
	sc, err := demo.LoadScenario(path)
	if err != nil {
		t.Fatal(err)
	}
	sim, err := demo.NewSim(t.Context(), sc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := sim.Close(); err != nil {
			t.Errorf("close sim: %v", err)
		}
	})
	if err := sim.Play(t.Context()); err != nil {
		t.Errorf("Play(%s): %v", path, err)
	}
	return sim
}

func TestEveryScenarioMeetsItsCheckpoints(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join(scenarioDir, "*.toml"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no scenarios in %s: %v", scenarioDir, err)
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) { play(t, path) })
	}
}

func TestTheHappyPathsAgentLogsParseIntoMetrics(t *testing.T) {
	sim := play(t, filepath.Join(scenarioDir, "happy.toml"))

	logs := sim.RunLogs()
	if len(logs) != 3 {
		t.Fatalf("RunLogs() = %d logs, want 3", len(logs))
	}
	for _, log := range logs {
		metrics, err := agentlog.ParseMetrics(log)
		if err != nil {
			t.Fatalf("ParseMetrics(%s): %v", log, err)
		}
		if !metrics.Settled || metrics.TokensIn == 0 || metrics.TokensOut == 0 || metrics.Turns == 0 {
			t.Errorf("ParseMetrics(%s) = %+v, want settled with non-zero tokens and turns", log, metrics)
		}
	}
}

func TestTwoRunsOfOneSeedPassThroughTheSameStates(t *testing.T) {
	path := filepath.Join(scenarioDir, "happy.toml")
	first, second := play(t, path), play(t, path)
	if !slices.Equal(first.Transitions(), second.Transitions()) {
		t.Errorf("Transitions() differ between runs of one seed:\nfirst:  %v\nsecond: %v", first.Transitions(), second.Transitions())
	}
	if a, b := metricsOf(t, first), metricsOf(t, second); !slices.Equal(a, b) {
		t.Errorf("agent metrics differ between runs of one seed:\nfirst:  %v\nsecond: %v", a, b)
	}
}

func metricsOf(t *testing.T, sim *demo.Sim) []int64 {
	t.Helper()
	var totals []int64
	for _, log := range sim.RunLogs() {
		metrics, err := agentlog.ParseMetrics(log)
		if err != nil {
			t.Fatalf("ParseMetrics(%s): %v", log, err)
		}
		totals = append(totals, metrics.TokensIn, metrics.TokensOut)
	}
	return totals
}

func TestCancellingAQueuedTicketFromTheBoardReachesCancelled(t *testing.T) {
	sc, err := demo.LoadScenario(filepath.Join(scenarioDir, "showcase.toml"))
	if err != nil {
		t.Fatal(err)
	}
	sim, err := demo.NewSim(t.Context(), sc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := sim.Close(); err != nil {
			t.Errorf("close sim: %v", err)
		}
	})

	if err := sim.PlayTo(t.Context(), time.Minute); err != nil {
		t.Fatal(err)
	}

	last := ""
	for _, tr := range sim.Transitions() {
		if tr.Ticket == "cancelled" {
			last = tr.State
		}
	}
	if last != "cancelled" {
		t.Errorf("ticket cancelled is %q after the scripted cancel, want cancelled", last)
	}
}

func TestTheShowcaseHoldsEveryStateAtOnce(t *testing.T) {
	sc, err := demo.LoadScenario(filepath.Join(scenarioDir, "showcase.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if sc.Showcase == 0 {
		t.Fatal("showcase.toml names no showcase moment")
	}
	sim, err := demo.NewSim(t.Context(), sc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := sim.Close(); err != nil {
			t.Errorf("close sim: %v", err)
		}
	})

	if err := sim.PlayTo(t.Context(), time.Duration(sc.Showcase)); err != nil {
		t.Fatal(err)
	}

	onBoard := map[string]bool{}
	for _, state := range sim.Board() {
		onBoard[state] = true
	}
	for s := range plan.State(plan.StateCount) {
		if !onBoard[s.String()] {
			t.Errorf("state %q is not on the board at %s", s, time.Duration(sc.Showcase))
		}
	}
}
