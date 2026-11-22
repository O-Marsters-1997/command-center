package plan

import (
	"fmt"
	"maps"
	"slices"
)

type stateSpec struct {
	name  string
	label string
	glyph GlyphWord
	stage Stage
	verbs []Verb
}

var stateInfo = map[State]stateSpec{
	Blocked: {
		name: "blocked", label: "Blocked", glyph: GlyphBlocked, stage: StageBlocked,
		verbs: []Verb{VerbLaunch},
	},
	Ready: {
		name: "ready", label: "Ready to launch", glyph: GlyphReady, stage: StageReady,
		verbs: []Verb{VerbLaunch},
	},
	Queued: {
		name: "queued", label: "Queued", glyph: GlyphPending, stage: StageReady,
		verbs: []Verb{VerbCancel},
	},
	Running: {
		name: "running", label: "Agent running", glyph: GlyphRunning, stage: StageWorking,
		verbs: []Verb{VerbKill, VerbFollowUp},
	},
	Failed: {
		name: "failed", label: "Failed", glyph: GlyphFailed, stage: StageWorking,
		verbs: []Verb{VerbReRun, VerbFollowUp},
	},
	PushPending: {name: "push_pending", label: "Waiting to push", glyph: GlyphPending, stage: StageWorking},
	AwaitingPR: {
		name: "awaiting_pr", label: "Awaiting PR", glyph: GlyphAttention, stage: StagePR,
		verbs: []Verb{VerbRaise, VerbFollowUp, VerbReRun},
	},
	Checking: {
		name: "checking", label: "Waiting on checks", glyph: GlyphChecking, stage: StagePR,
		verbs: []Verb{VerbReRun, VerbFollowUp},
	},
	NeedsYou: {
		name: "needs_you", label: "Needs you", glyph: GlyphAttention, stage: StagePR,
		verbs: []Verb{VerbReRun, VerbFollowUp, VerbKill, VerbAdoptHead},
	},
	PushFailed: {
		name: "push_failed", label: "Push failed", glyph: GlyphFailed, stage: StageWorking,
		verbs: []Verb{VerbRetryPush, VerbReRun, VerbFollowUp},
	},
	ReviewMe: {
		name: "review_me", label: "Ready for review", glyph: GlyphAttention, stage: StageReview,
		verbs: []Verb{VerbFollowUp},
	},
	PRMerged: {
		name: "merged", label: "Merged", glyph: GlyphDone, stage: StageMerged,
		verbs: []Verb{VerbRemoveWorktree},
	},
	Withdrawn: {
		name: "withdrawn", label: "PR closed", glyph: GlyphFailed,
		verbs: []Verb{VerbReRun, VerbFollowUp, VerbRemoveWorktree},
	},
	Cancelled: {name: "cancelled", label: "Cancelled", glyph: GlyphReady, verbs: []Verb{VerbLaunch}},
	BaseMoved: {
		name: "base_moved", label: "Base moved", glyph: GlyphPending, stage: StagePR,
		verbs: []Verb{VerbRefresh, VerbReRun, VerbFollowUp},
	},
	CIFailed: {
		name: "ci_failed", label: "Checks failing", glyph: GlyphFailed, stage: StagePR,
		verbs: []Verb{VerbReRun, VerbFollowUp, VerbAdoptHead},
	},
	RefreshConflicted: {
		name: "refresh_conflicted", label: "Refresh conflicted", glyph: GlyphFailed, stage: StagePR,
		verbs: []Verb{VerbResolve, VerbAbort},
	},
	ConflictsWithMain: {
		name: "conflicts_with_main", label: "Conflicts with main", glyph: GlyphFailed, stage: StagePR,
		verbs: []Verb{VerbRefresh},
	},
	VerificationFailed: {
		name: "verification_failed", label: "Verification failed", glyph: GlyphFailed, stage: StageWorking,
		verbs: []Verb{VerbRetryPush, VerbReRun, VerbFollowUp},
	},
	WaitingOnProducerDeploy: {
		name: "waiting_on_producer_deploy", label: "Waiting on deploy", glyph: GlyphBlocked, stage: StagePR,
		verbs: []Verb{VerbReRun, VerbFollowUp},
	},
	AwaitingInput: {
		name: "awaiting_input", label: "Asked you a question", glyph: GlyphAttention, stage: StageWorking,
		verbs: []Verb{VerbFollowUp, VerbReRun},
	},
	PushRefused: {
		name: "push_refused", label: "Push refused", glyph: GlyphAttention, stage: StageWorking,
		verbs: []Verb{VerbRetryPush, VerbReRun, VerbFollowUp},
	},
	Verifying: {
		name: "verifying", label: "Verifying", glyph: GlyphRunning, stage: StageWorking,
	},
	WaitingToVerify: {
		name: "waiting_to_verify", label: "Waiting to verify", glyph: GlyphPending, stage: StageWorking,
	},
	Parked: {
		name: "parked", label: "Parked", glyph: GlyphAttention, stage: StagePR,
		verbs: []Verb{VerbReRun, VerbFollowUp},
	},
}

var unknownSpec = stateSpec{name: "unknown", label: "Unknown", glyph: GlyphFailed}

func (s State) spec() stateSpec {
	if info, ok := stateInfo[s]; ok {
		return info
	}
	return unknownSpec
}

func (s State) String() string { return s.spec().name }

func (s State) MarshalText() ([]byte, error) { return []byte(s.String()), nil }

func (s State) FailingChecks() bool { return s == CIFailed || s == Parked }

func (s State) Label() string { return s.spec().label }

func (s State) Glyph() GlyphWord { return s.spec().glyph }

func (s State) Stage() Stage { return s.spec().stage }

func Verbs(s State) []Verb {
	return slices.Clone(stateInfo[s].verbs)
}

func AllStates() []State {
	return slices.Sorted(maps.Keys(stateInfo))
}

func ParseState(s string) (State, error) {
	for state, info := range stateInfo {
		if info.name == s {
			return state, nil
		}
	}
	return StateUnknown, fmt.Errorf("unknown state %q", s)
}
