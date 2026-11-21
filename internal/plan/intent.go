package plan

import (
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan/option"
)

type IntentOutcome string

const (
	IntentInProgress IntentOutcome = ""
	IntentDone       IntentOutcome = "done"
	IntentFailed     IntentOutcome = "failed"
	IntentWithdrawn  IntentOutcome = "withdrawn"
)

type QueuedBy string

const (
	QueuedByOwner QueuedBy = "owner"
	QueuedByLoop  QueuedBy = "loop"
)

type IntentPhase string

const (
	PhaseQueued   IntentPhase = "queued"
	PhaseHeld     IntentPhase = "held"
	PhasePickedUp IntentPhase = "picked_up"
	PhaseRunning  IntentPhase = "running"
	PhaseFinished IntentPhase = "finished"
)

type IntentRecord struct {
	ID          int64
	Ticket      TicketURL
	Verb        Verb
	QueuedBy    QueuedBy
	At          time.Time
	HoldUntil   time.Time
	ConsumedAt  option.Option[time.Time]
	FinishedAt  option.Option[time.Time]
	Outcome     IntentOutcome
	Detail      string
	Run         option.Option[RunID]
	TicketState State
}

type IntentStatus struct {
	Record  IntentRecord
	Phase   IntentPhase
	Outcome IntentOutcome
	Detail  string
	Glyph   GlyphWord
	Ended   option.Option[time.Time]
}

func StatusOf(rec IntentRecord, run option.Option[RunSummary], now time.Time) IntentStatus {
	st := IntentStatus{Record: rec, Outcome: rec.Outcome, Detail: rec.Detail}
	switch {
	case rec.Outcome == IntentWithdrawn:
		st.Phase = PhaseFinished
		st.Ended = rec.FinishedAt
		if st.Ended.IsNone() {
			st.Ended = rec.ConsumedAt
		}
	case rec.ConsumedAt.IsNone():
		st.Phase = PhaseQueued
		if rec.HoldUntil.After(now) {
			st.Phase = PhaseHeld
		}
	case rec.Run.IsSome():
		st.Phase = PhaseRunning
		st.Outcome = IntentInProgress
		if run.IsSome() && run.Unwrap().Outcome.IsSome() {
			s := run.Unwrap()
			st.Phase = PhaseFinished
			st.Ended = s.EndedAt
			st.Outcome = IntentFailed
			if s.Outcome.Unwrap().Glyph() == GlyphDone {
				st.Outcome = IntentDone
			}
		}
	case rec.FinishedAt.IsSome():
		st.Phase = PhaseFinished
		st.Ended = rec.FinishedAt
		if st.Outcome == IntentInProgress {
			st.Outcome = IntentDone
		}
	default:
		st.Phase = PhasePickedUp
	}
	st.Glyph = intentGlyph(st)
	return st
}

func railIntent(rec IntentRecord, latestRun RunSummary, state State, now time.Time) option.Option[IntentStatus] {
	run := option.None[RunSummary]()
	if rec.Run.IsSome() && rec.Run.Unwrap() == latestRun.ID {
		run = option.Some(latestRun)
	}
	st := StatusOf(rec, run, now)
	if st.Outcome == IntentInProgress || (st.Outcome == IntentFailed && state == rec.TicketState) {
		return option.Some(st)
	}
	return option.None[IntentStatus]()
}

func intentGlyph(st IntentStatus) GlyphWord {
	switch {
	case st.Outcome == IntentDone:
		return GlyphDone
	case st.Outcome == IntentFailed:
		return GlyphFailed
	case st.Phase == PhaseRunning:
		return GlyphRunning
	}
	return GlyphPending
}

type IntentStep struct {
	Phase   IntentPhase
	Outcome IntentOutcome
	At      time.Time
}

func Trail(st IntentStatus) []IntentStep {
	rec := st.Record
	steps := []IntentStep{{Phase: PhaseQueued, At: rec.At}}
	if st.Phase == PhaseHeld {
		steps = append(steps, IntentStep{Phase: PhaseHeld, At: rec.At})
	}
	if st.Outcome == IntentWithdrawn {
		return append(steps, IntentStep{Phase: PhaseFinished, Outcome: IntentWithdrawn, At: st.Ended.UnwrapOr(rec.At)})
	}
	if st.Phase == PhaseQueued || st.Phase == PhaseHeld {
		return steps
	}
	picked := rec.ConsumedAt.UnwrapOr(rec.At)
	steps = append(steps, IntentStep{Phase: PhasePickedUp, At: picked})
	if rec.Run.IsSome() {
		steps = append(steps, IntentStep{Phase: PhaseRunning, At: picked})
	}
	if st.Phase == PhaseFinished {
		steps = append(steps, IntentStep{Phase: PhaseFinished, Outcome: st.Outcome, At: st.Ended.UnwrapOr(picked)})
	}
	return steps
}
