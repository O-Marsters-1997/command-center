package plan

import (
	"fmt"
	"maps"
	"slices"
)

type stateSpec struct {
	name       string
	label      string
	glyph      GlyphWord
	verbs      []Verb
	unattended bool
}

var stateInfo = map[State]stateSpec{
	Blocked: {name: "blocked", label: "Blocked", glyph: GlyphBlocked, verbs: []Verb{VerbLaunch}},
	Ready:   {name: "ready", label: "Ready to launch", glyph: GlyphReady, verbs: []Verb{VerbLaunch}},
	Queued: {
		name: "queued", label: "Queued", glyph: GlyphPending, verbs: []Verb{VerbCancel}, unattended: true,
	},
	Running: {
		name: "running", label: "Agent running", glyph: GlyphRunning,
		verbs: []Verb{VerbKill, VerbFollowUp}, unattended: true,
	},
	Failed: {name: "failed", label: "Failed", glyph: GlyphFailed, verbs: []Verb{VerbReRun, VerbFollowUp}},
	CutFailed: {
		name: "cut_failed", label: "Cut failed", glyph: GlyphFailed, verbs: []Verb{VerbReRun, VerbFollowUp},
	},
	PushPending: {name: "push_pending", label: "Waiting to push", glyph: GlyphPending, unattended: true},
	Checking: {
		name: "checking", label: "Waiting on checks", glyph: GlyphChecking,
		verbs: []Verb{VerbReRun, VerbFollowUp}, unattended: true,
	},
	NeedsYou: {
		name: "needs_you", label: "Needs you", glyph: GlyphAttention,
		verbs: []Verb{VerbReRun, VerbFollowUp, VerbKill},
	},
	PushFailed: {
		name: "push_failed", label: "Push failed", glyph: GlyphFailed,
		verbs: []Verb{VerbRetryPush, VerbReRun, VerbFollowUp},
	},
	ReviewMe: {name: "review_me", label: "Ready for review", glyph: GlyphAttention},
	PRMerged: {name: "merged", label: "Merged", glyph: GlyphDone, verbs: []Verb{VerbRemoveWorktree}},
	PRClosedUnmerged: {
		name: "pr_closed_unmerged", label: "PR closed", glyph: GlyphFailed,
		verbs: []Verb{VerbReRun, VerbFollowUp, VerbRemoveWorktree},
	},
	BaseGone: {
		name: "base_gone", label: "Base gone", glyph: GlyphFailed,
		verbs: []Verb{VerbReRun, VerbFollowUp, VerbRemoveWorktree},
	},
	Cancelled: {name: "cancelled", label: "Cancelled", glyph: GlyphReady, verbs: []Verb{VerbLaunch}},
	BaseMoved: {
		name: "base_moved", label: "Base moved", glyph: GlyphPending,
		verbs: []Verb{VerbRefresh, VerbReRun, VerbFollowUp}, unattended: true,
	},
	CIFailed: {
		name: "ci_failed", label: "Checks failing", glyph: GlyphFailed, verbs: []Verb{VerbReRun, VerbFollowUp},
	},
	RefreshConflicted: {
		name: "refresh_conflicted", label: "Refresh conflicted", glyph: GlyphFailed, verbs: []Verb{VerbAbort},
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
}

func (s State) String() string {
	info, ok := stateInfo[s]
	if !ok {
		return "unknown"
	}
	return info.name
}

func (s State) Label() string {
	info, ok := stateInfo[s]
	if !ok {
		return "Unknown"
	}
	return info.label
}

func (s State) Glyph() GlyphWord {
	info, ok := stateInfo[s]
	if !ok {
		return GlyphFailed
	}
	return info.glyph
}

func (s State) Unattended() bool {
	return stateInfo[s].unattended
}

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
