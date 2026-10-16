package plan

import (
	"fmt"
	"maps"
	"slices"
)

type stateSpec struct {
	name       string
	glyph      GlyphWord
	verbs      []Verb
	unattended bool
}

var stateInfo = map[State]stateSpec{
	Blocked: {name: "blocked", glyph: GlyphBlocked, verbs: []Verb{VerbLaunch}},
	Ready:   {name: "ready", glyph: GlyphReady, verbs: []Verb{VerbLaunch}},
	Queued: {
		name: "queued", glyph: GlyphPending, verbs: []Verb{VerbCancel}, unattended: true,
	},
	Running: {
		name: "running", glyph: GlyphRunning, verbs: []Verb{VerbKill, VerbFollowUp}, unattended: true,
	},
	Failed:      {name: "failed", glyph: GlyphFailed, verbs: []Verb{VerbReRun, VerbFollowUp}},
	CutFailed:   {name: "cut_failed", glyph: GlyphFailed, verbs: []Verb{VerbReRun, VerbFollowUp}},
	PushPending: {name: "push_pending", glyph: GlyphPending, unattended: true},
	Checking: {
		name: "checking", glyph: GlyphChecking, verbs: []Verb{VerbReRun, VerbFollowUp}, unattended: true,
	},
	NeedsYou: {name: "needs_you", glyph: GlyphAttention, verbs: []Verb{VerbReRun, VerbFollowUp, VerbKill}},
	PushFailed: {
		name: "push_failed", glyph: GlyphFailed, verbs: []Verb{VerbRetryPush, VerbReRun, VerbFollowUp},
	},
	ReviewMe: {name: "review_me", glyph: GlyphAttention},
	PRMerged: {name: "merged", glyph: GlyphDone, verbs: []Verb{VerbRemoveWorktree}},
	PRClosedUnmerged: {
		name: "pr_closed_unmerged", glyph: GlyphFailed,
		verbs: []Verb{VerbReRun, VerbFollowUp, VerbRemoveWorktree},
	},
	BaseGone: {
		name: "base_gone", glyph: GlyphFailed,
		verbs: []Verb{VerbReRun, VerbFollowUp, VerbRemoveWorktree},
	},
	Cancelled: {name: "cancelled", glyph: GlyphReady, verbs: []Verb{VerbLaunch}},
	BaseMoved: {
		name: "base_moved", glyph: GlyphPending,
		verbs: []Verb{VerbRefresh, VerbReRun, VerbFollowUp}, unattended: true,
	},
	CIFailed:          {name: "ci_failed", glyph: GlyphFailed, verbs: []Verb{VerbReRun, VerbFollowUp}},
	RefreshConflicted: {name: "refresh_conflicted", glyph: GlyphFailed, verbs: []Verb{VerbAbort}},
	ConflictsWithMain: {name: "conflicts_with_main", glyph: GlyphFailed, verbs: []Verb{VerbRefresh}},
	VerificationFailed: {
		name: "verification_failed", glyph: GlyphFailed,
		verbs: []Verb{VerbRetryPush, VerbReRun, VerbFollowUp},
	},
	WaitingOnProducerDeploy: {
		name: "waiting_on_producer_deploy", glyph: GlyphBlocked, verbs: []Verb{VerbReRun, VerbFollowUp},
	},
	AwaitingInput: {name: "awaiting_input", glyph: GlyphAttention, verbs: []Verb{VerbFollowUp, VerbReRun}},
}

func (s State) String() string {
	info, ok := stateInfo[s]
	if !ok {
		return "unknown"
	}
	return info.name
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
