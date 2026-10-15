package loop_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/cctest"
	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

func TestAbsorbEvents(t *testing.T) {
	t.Parallel()

	type eventFields struct {
		URL    string
		Detail string
		At     time.Time
	}
	type step struct {
		observed plan.Observation
		want     []eventFields
	}
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "owner/cc-sandbox", Branch: "cc-1-first"}
	ref := branchRef(ticket.Repo, ticket.Branch)
	pr := func(state plan.PRState, checks ...plan.CheckState) plan.Observation {
		byName := make(map[string]plan.CheckState, len(checks))
		for _, check := range checks {
			byName[check.Name] = check
		}
		obs := plan.NewObservation(testAt)
		obs.PRs[ref] = plan.PR{Number: 41, State: state, Checks: byName}
		return obs
	}
	opened := pr(plan.Open)
	mergedAt := testAt.Add(-26*time.Hour - 30*time.Minute)
	merged := plan.NewObservation(testAt)
	merged.PRs[ref] = plan.PR{Number: 41, State: plan.Merged, MergedAt: mergedAt}
	resetPrevious := func(t *testing.T, store *storepkg.Store) {
		t.Helper()
		if err := store.SaveObservation(t.Context(), plan.NewObservation(testAt)); err != nil {
			t.Fatalf("reset observation: %v", err)
		}
	}
	keepPrevious := func(*testing.T, *storepkg.Store) {}
	pending := plan.CheckState{Name: "Build", Status: "IN_PROGRESS"}
	passed := plan.CheckState{Name: "Build", Status: "COMPLETED", Conclusion: "SUCCESS"}
	failed := plan.CheckState{Name: "Lint", Status: "COMPLETED", Conclusion: "FAILURE"}
	skipped := plan.CheckState{Name: "Optional", Status: "COMPLETED", Conclusion: "SKIPPED"}
	openedEvent := eventFields{URL: ticket.URL, Detail: "PR #41 https://github.com/owner/cc-sandbox/pull/41", At: testAt}
	mergedEvent := eventFields{URL: ticket.URL, Detail: "PR #41 merged", At: mergedAt}
	buildPassed := eventFields{URL: ticket.URL, Detail: "Build passed", At: testAt}
	lintFailed := eventFields{URL: ticket.URL, Detail: "Lint failed", At: testAt}
	optionalSkipped := eventFields{URL: ticket.URL, Detail: "Optional skipped", At: testAt}
	tests := []struct {
		name      string
		kind      storepkg.EventKind
		initial   plan.Observation
		steps     []step
		afterTick func(*testing.T, *storepkg.Store)
	}{
		{
			name:      "merged PR is recorded once at its merge time even when the previous observation is cleared",
			kind:      storepkg.EventPRMerged,
			afterTick: resetPrevious,
			steps: []step{
				{observed: merged, want: []eventFields{mergedEvent}},
				{observed: merged, want: []eventFields{mergedEvent}},
			},
		},
		{
			name:      "closed unmerged PR has no merge event",
			kind:      storepkg.EventPRMerged,
			afterTick: resetPrevious,
			steps: []step{
				{observed: pr(plan.Closed)},
				{observed: pr(plan.Closed)},
			},
		},
		{
			name:      "PR opening is recorded on appearance and not repeated",
			kind:      storepkg.EventPROpened,
			afterTick: keepPrevious,
			steps: []step{
				{observed: plan.NewObservation(testAt)},
				{observed: opened, want: []eventFields{openedEvent}},
				{observed: opened, want: []eventFields{openedEvent}},
			},
		},
		{
			name:      "PR already observed before restart has no opening event",
			kind:      storepkg.EventPROpened,
			initial:   opened,
			afterTick: keepPrevious,
			steps: []step{
				{observed: opened},
				{observed: opened},
				{observed: opened},
			},
		},
		{
			name:      "checks landing, remaining terminal, rerunning and skipping",
			kind:      storepkg.EventChecksLanded,
			afterTick: keepPrevious,
			steps: []step{
				{observed: pr(plan.Open, pending)},
				{observed: pr(plan.Open, passed, failed), want: []eventFields{buildPassed, lintFailed}},
				{observed: pr(plan.Open, passed, failed), want: []eventFields{buildPassed, lintFailed}},
				{observed: pr(plan.Open, pending, failed), want: []eventFields{buildPassed, lintFailed}},
				{observed: pr(plan.Open, passed, failed), want: []eventFields{buildPassed, lintFailed, buildPassed}},
				{observed: pr(plan.Open, passed, failed, skipped),
					want: []eventFields{buildPassed, lintFailed, buildPassed, optionalSkipped}},
			},
		},
		{
			name:      "terminal check already observed before restart has no landing event",
			kind:      storepkg.EventChecksLanded,
			initial:   pr(plan.Open, passed),
			afterTick: keepPrevious,
			steps:     []step{{observed: pr(plan.Open, passed)}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			store := openStore(t)
			if err := store.UpsertTickets(ctx, []storepkg.Ticket{ticket}); err != nil {
				t.Fatalf("UpsertTickets: %v", err)
			}
			if err := store.SaveObservation(ctx, tt.initial); err != nil {
				t.Fatalf("SaveObservation: %v", err)
			}
			observed := plan.NewObservation(testAt)
			lp := newLoop(store, func(context.Context) (plan.Observation, error) { return observed, nil },
				cctest.Clock{At: testAt}, config.Config{}, config.Workspace{}, runner.ProcessRunner{})
			for tick, step := range tt.steps {
				observed = step.observed
				if err := lp.RunOnce(ctx); err != nil {
					t.Fatalf("RunOnce tick %d: %v", tick, err)
				}
				events, err := store.Events(ctx)
				if err != nil {
					t.Fatalf("Events: %v", err)
				}
				var got []eventFields
				for _, event := range eventsOfKind(events, tt.kind) {
					got = append(got, eventFields{URL: event.TicketURL, Detail: event.Detail, At: event.At})
				}
				if diff := cmp.Diff(step.want, got); diff != "" {
					t.Errorf("%s after tick %d mismatch (-want +got):\n%s", tt.kind, tick, diff)
				}
				tt.afterTick(t, store)
			}
		})
	}
}

func eventsOfKind(events []storepkg.Event, kind storepkg.EventKind) []storepkg.Event {
	var matching []storepkg.Event
	for _, event := range events {
		if event.Kind == kind {
			matching = append(matching, event)
		}
	}
	return matching
}
