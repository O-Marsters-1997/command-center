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
	verbs []Verb
}

var stateInfo = map[State]stateSpec{
	Blocked: {name: "blocked", label: "Blocked", glyph: GlyphBlocked, verbs: []Verb{VerbLaunch}},
	Ready:   {name: "ready", label: "Ready to launch", glyph: GlyphReady, verbs: []Verb{VerbLaunch}},
	Queued:  {name: "queued", label: "Queued", glyph: GlyphPending, verbs: []Verb{VerbCancel}},
	Running: {
		name: "running", label: "Agent running", glyph: GlyphRunning,
		verbs: []Verb{VerbKill, VerbFollowUp},
	},
	Failed:      {name: "failed", label: "Failed", glyph: GlyphFailed, verbs: []Verb{VerbReRun, VerbFollowUp}},
	PushPending: {name: "push_pending", label: "Waiting to push", glyph: GlyphPending},
	AwaitingPR: {
		name: "awaiting_pr", label: "Awaiting PR", glyph: GlyphAttention,
		verbs: []Verb{VerbRaise, VerbFollowUp, VerbReRun},
	},
	Checking: {
		name: "checking", label: "Waiting on checks", glyph: GlyphChecking,
		verbs: []Verb{VerbReRun, VerbFollowUp},
	},
	NeedsYou: {
		name: "needs_you", label: "Needs you", glyph: GlyphAttention,
		verbs: []Verb{VerbReRun, VerbFollowUp, VerbKill, VerbAdoptHead},
	},
	PushFailed: {
		name: "push_failed", label: "Push failed", glyph: GlyphFailed,
		verbs: []Verb{VerbRetryPush, VerbReRun, VerbFollowUp},
	},
	ReviewMe: {
		name: "review_me", label: "Ready for review", glyph: GlyphAttention, verbs: []Verb{VerbFollowUp},
	},
	PRMerged: {name: "merged", label: "Merged", glyph: GlyphDone, verbs: []Verb{VerbRemoveWorktree}},
	Withdrawn: {
		name: "withdrawn", label: "PR closed", glyph: GlyphFailed,
		verbs: []Verb{VerbReRun, VerbFollowUp, VerbRemoveWorktree},
	},
	Cancelled: {name: "cancelled", label: "Cancelled", glyph: GlyphReady, verbs: []Verb{VerbLaunch}},
	BaseMoved: {
		name: "base_moved", label: "Base moved", glyph: GlyphPending,
		verbs: []Verb{VerbRefresh, VerbReRun, VerbFollowUp},
	},
	CIFailed: {
		name: "ci_failed", label: "Checks failing", glyph: GlyphFailed,
		verbs: []Verb{VerbReRun, VerbFollowUp, VerbAdoptHead},
	},
	RefreshConflicted: {
		name: "refresh_conflicted", label: "Refresh conflicted", glyph: GlyphFailed,
		verbs: []Verb{VerbResolve, VerbAbort},
	},
	ConflictsWithMain: {
		name: "conflicts_with_main", label: "Conflicts with main", glyph: GlyphFailed, verbs: []Verb{VerbRefresh},
	},
	VerificationFailed: {
		name: "verification_failed", label: "Verification failed", glyph: GlyphFailed,
		verbs: []Verb{VerbRetryPush, VerbReRun, VerbFollowUp},
	},
	WaitingOnProducerDeploy: {
		name: "waiting_on_producer_deploy", label: "Waiting on deploy", glyph: GlyphBlocked,
		verbs: []Verb{VerbReRun, VerbFollowUp},
	},
	AwaitingInput: {
		name: "awaiting_input", label: "Asked you a question", glyph: GlyphAttention,
		verbs: []Verb{VerbFollowUp, VerbReRun},
	},
	PushRefused: {
		name: "push_refused", label: "Push refused", glyph: GlyphAttention,
		verbs: []Verb{VerbRetryPush, VerbReRun, VerbFollowUp},
	},
	Verifying: {
		name: "verifying", label: "Verifying", glyph: GlyphRunning,
	},
	WaitingToVerify: {
		name: "waiting_to_verify", label: "Waiting to verify", glyph: GlyphPending,
	},
	Parked: {
		name: "parked", label: "Parked", glyph: GlyphAttention, verbs: []Verb{VerbReRun, VerbFollowUp},
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
