package plan_test

import (
	"slices"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
)

var equateOptions = cmpopts.EquateComparable(option.Option[time.Time]{}, option.Option[plan.RunID]{})

var allGlyphWords = []plan.GlyphWord{
	plan.GlyphFailed, plan.GlyphAttention, plan.GlyphRunning, plan.GlyphPending,
	plan.GlyphChecking, plan.GlyphReady, plan.GlyphBlocked, plan.GlyphDone,
}

func TestStatusOf(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	at := now.Add(-10 * time.Minute)
	consumed := now.Add(-8 * time.Minute)
	finished := now.Add(-5 * time.Minute)

	base := plan.IntentRecord{ID: 1, Ticket: "sandbox://T-1", Verb: plan.VerbRefresh, QueuedBy: plan.QueuedByOwner, At: at}
	held := base
	held.HoldUntil = now.Add(time.Minute)
	elapsed := base
	elapsed.HoldUntil = now.Add(-time.Second)
	picked := base
	picked.ConsumedAt = option.Some(consumed)
	agent := picked
	agent.Verb = plan.VerbReRun
	agent.Run = option.Some(plan.RunID(7))
	gitDone := picked
	gitDone.FinishedAt = option.Some(finished)
	gitDone.Outcome = plan.IntentDone
	gitFailed := gitDone
	gitFailed.Outcome = plan.IntentFailed
	gitFailed.Detail = "merge conflict"
	withdrawn := picked
	withdrawn.Outcome = plan.IntentWithdrawn
	withdrawn.FinishedAt = option.Some(consumed)

	stale := agent
	stale.Outcome = plan.IntentFailed
	queuedWithdrawn := base
	queuedWithdrawn.Outcome = plan.IntentWithdrawn
	queuedWithdrawn.ConsumedAt = option.Some(finished)

	ended := func(o plan.Outcome) option.Option[plan.RunSummary] {
		return option.Some(plan.RunSummary{ID: 7, Outcome: option.Some(o), EndedAt: option.Some(finished)})
	}
	noRun := option.None[plan.RunSummary]()
	runningRun := option.Some(plan.RunSummary{ID: 7})

	pending := func(p plan.IntentPhase) plan.IntentStatus {
		return plan.IntentStatus{Phase: p, Glyph: plan.GlyphPending}
	}
	running := plan.IntentStatus{Phase: plan.PhaseRunning, Glyph: plan.GlyphRunning}
	finishedDone := plan.IntentStatus{
		Phase: plan.PhaseFinished, Outcome: plan.IntentDone, Glyph: plan.GlyphDone, Ended: option.Some(finished),
	}
	finishedFailed := plan.IntentStatus{
		Phase: plan.PhaseFinished, Outcome: plan.IntentFailed, Glyph: plan.GlyphFailed, Ended: option.Some(finished),
	}
	failedWithDetail := finishedFailed
	failedWithDetail.Detail = "merge conflict"
	withdrawnAt := func(end time.Time) plan.IntentStatus {
		return plan.IntentStatus{
			Phase: plan.PhaseFinished, Outcome: plan.IntentWithdrawn, Glyph: plan.GlyphPending, Ended: option.Some(end),
		}
	}

	tests := []struct {
		name string
		rec  plan.IntentRecord
		run  option.Option[plan.RunSummary]
		want plan.IntentStatus
	}{
		{"queued", base, noRun, pending(plan.PhaseQueued)},
		{"held", held, noRun, pending(plan.PhaseHeld)},
		{"hold elapsed", elapsed, noRun, pending(plan.PhaseQueued)},
		{"picked up git verb", picked, noRun, pending(plan.PhasePickedUp)},
		{"running agent verb", agent, runningRun, running},
		{"agent run not loaded", agent, noRun, running},
		{"running ignores stored outcome", stale, runningRun, running},
		{"withdrawn from queue", queuedWithdrawn, noRun, withdrawnAt(finished)},
		{"done git verb", gitDone, noRun, finishedDone},
		{"failed with detail", gitFailed, noRun, failedWithDetail},
		{"withdrawn", withdrawn, noRun, withdrawnAt(consumed)},
		{"agent run pushed", agent, ended(plan.OutcomePush), finishedDone},
		{"agent run continued", agent, ended(plan.OutcomeContinued), finishedDone},
		{"agent run passed", agent, ended(plan.OutcomePassed), finishedDone},
		{"agent run failed", agent, ended(plan.OutcomeFailed), finishedFailed},
		{"agent run asked", agent, ended(plan.OutcomeAsked), finishedFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := plan.StatusOf(tt.rec, tt.run, now)
			want := tt.want
			want.Record = tt.rec
			if diff := cmp.Diff(want, got, equateOptions); diff != "" {
				t.Errorf("StatusOf() mismatch (-want +got):\n%s", diff)
			}
			if !slices.Contains(allGlyphWords, got.Glyph) {
				t.Errorf("StatusOf().Glyph = %q, want one of the eight words", got.Glyph)
			}
		})
	}
}

func TestTrail(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	at := now.Add(-10 * time.Minute)
	consumed := now.Add(-8 * time.Minute)
	finished := now.Add(-5 * time.Minute)
	noRun := option.None[plan.RunSummary]()

	base := plan.IntentRecord{Verb: plan.VerbRefresh, At: at}
	held := base
	held.HoldUntil = now.Add(time.Minute)
	git := base
	git.ConsumedAt = option.Some(consumed)
	git.FinishedAt = option.Some(finished)
	git.Outcome = plan.IntentDone
	agent := base
	agent.Verb = plan.VerbResolve
	agent.ConsumedAt = option.Some(consumed)
	agent.Run = option.Some(plan.RunID(3))
	withdrawn := git
	withdrawn.Outcome = plan.IntentWithdrawn
	withdrawn.FinishedAt = option.Some(consumed)
	failedRun := option.Some(plan.RunSummary{Outcome: option.Some(plan.OutcomeFailed), EndedAt: option.Some(finished)})

	tests := []struct {
		name string
		st   plan.IntentStatus
		want []plan.IntentStep
	}{
		{"queued", plan.StatusOf(base, noRun, now), []plan.IntentStep{
			{Phase: plan.PhaseQueued, At: at}}},
		{"held", plan.StatusOf(held, noRun, now), []plan.IntentStep{
			{Phase: plan.PhaseQueued, At: at}, {Phase: plan.PhaseHeld, At: at}}},
		{"git verb skips running", plan.StatusOf(git, noRun, now), []plan.IntentStep{
			{Phase: plan.PhaseQueued, At: at}, {Phase: plan.PhasePickedUp, At: consumed},
			{Phase: plan.PhaseFinished, Outcome: plan.IntentDone, At: finished}}},
		{"agent verb running", plan.StatusOf(agent, noRun, now), []plan.IntentStep{
			{Phase: plan.PhaseQueued, At: at}, {Phase: plan.PhasePickedUp, At: consumed},
			{Phase: plan.PhaseRunning, At: consumed}}},
		{"agent verb finished", plan.StatusOf(agent, failedRun, now), []plan.IntentStep{
			{Phase: plan.PhaseQueued, At: at}, {Phase: plan.PhasePickedUp, At: consumed},
			{Phase: plan.PhaseRunning, At: consumed},
			{Phase: plan.PhaseFinished, Outcome: plan.IntentFailed, At: finished}}},
		{"withdrawn skips picked up", plan.StatusOf(withdrawn, noRun, now), []plan.IntentStep{
			{Phase: plan.PhaseQueued, At: at},
			{Phase: plan.PhaseFinished, Outcome: plan.IntentWithdrawn, At: consumed}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := plan.Trail(tt.st)
			if diff := cmp.Diff(tt.want, got, equateOptions); diff != "" {
				t.Errorf("Trail() mismatch (-want +got):\n%s", diff)
			}
			if !slices.IsSortedFunc(got, func(a, b plan.IntentStep) int { return a.At.Compare(b.At) }) {
				t.Errorf("Trail() not in time order: %v", got)
			}
		})
	}
}
