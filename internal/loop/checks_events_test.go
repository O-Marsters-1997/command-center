package loop_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

func TestRecordChecksLandedAppendsOnePerCheckThatLands(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "owner/cc-sandbox", Branch: "cc-1-first"}
	if err := store.UpsertTickets(ctx, []storepkg.Ticket{ticket}); err != nil {
		t.Fatalf("UpsertTickets: %v", err)
	}

	pending := plan.CheckState{Name: "Build", Status: "IN_PROGRESS"}
	passed := plan.CheckState{Name: "Build", Status: "COMPLETED", Conclusion: "SUCCESS"}
	failed := plan.CheckState{Name: "Lint", Status: "COMPLETED", Conclusion: "FAILURE"}
	observe := func(checks ...plan.CheckState) plan.Observation {
		byName := map[string]plan.CheckState{}
		for _, c := range checks {
			byName[c.Name] = c
		}
		return plan.Observation{PRs: map[plan.BranchRef]plan.PR{
			branchRef(ticket.Repo, ticket.Branch): {Number: 41, State: plan.Open, Checks: byName},
		}}
	}

	observed := observe(pending)
	lp := newLoop(store,
		func(context.Context) (plan.Observation, error) { return observed, nil },
		fixedClock(time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)), config.Config{}, config.Workspace{},
		runner.ProcessRunner{})

	landed := func(obs plan.Observation) []string {
		t.Helper()
		observed = obs
		if err := lp.RunOnce(ctx); err != nil {
			t.Fatalf("RunOnce: %v", err)
		}
		events, err := store.Events(ctx)
		if err != nil {
			t.Fatalf("Events: %v", err)
		}
		var details []string
		for _, e := range events {
			if e.Kind == "checks_landed" {
				details = append(details, e.Detail)
			}
		}
		return details
	}

	check := func(stage string, got []string, want ...string) {
		t.Helper()
		if !slices.Equal(got, want) {
			t.Errorf("checks_landed %s = %q, want %q", stage, got, want)
		}
	}
	check("while pending", landed(observe(pending)))
	check("after both land", landed(observe(passed, failed)), "Build passed", "Lint failed")
	check("after an unchanged tick", landed(observe(passed, failed)), "Build passed", "Lint failed")
	landed(observe(pending, failed))
	check("after a re-run lands again", landed(observe(passed, failed)), "Build passed", "Lint failed", "Build passed")
}

func TestRecordChecksLandedIgnoresChecksAlreadyTerminalInThePreviousObservation(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "owner/cc-sandbox", Branch: "cc-1-first"}
	if err := store.UpsertTickets(ctx, []storepkg.Ticket{ticket}); err != nil {
		t.Fatalf("UpsertTickets: %v", err)
	}
	observed := plan.Observation{PRs: map[plan.BranchRef]plan.PR{
		branchRef(ticket.Repo, ticket.Branch): {Number: 41, State: plan.Open, Checks: map[string]plan.CheckState{
			"Build": {Name: "Build", Status: "COMPLETED", Conclusion: "SUCCESS"},
		}},
	}}
	if err := store.SaveObservation(ctx, observed); err != nil {
		t.Fatalf("SaveObservation: %v", err)
	}

	lp := newLoop(store,
		func(context.Context) (plan.Observation, error) { return observed, nil },
		fixedClock(time.Now()), config.Config{}, config.Workspace{}, runner.ProcessRunner{})
	if err := lp.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	events, err := store.Events(ctx)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	for _, e := range events {
		if e.Kind == "checks_landed" {
			t.Errorf("checks_landed after a restart with an unchanged observation = %+v, want none", e)
		}
	}
}
