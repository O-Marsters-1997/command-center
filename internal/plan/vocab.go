package plan

import (
	"fmt"
	"slices"
)

type Verb string

const (
	VerbUnknown        Verb = ""
	VerbLaunch         Verb = "launch"
	VerbKill           Verb = "kill"
	VerbReRun          Verb = "re-run"
	VerbRetryPush      Verb = "retry-push"
	VerbRemoveWorktree Verb = "remove-worktree"
	VerbCancel         Verb = "cancel"
	VerbRefresh        Verb = "refresh"
	VerbAbort          Verb = "abort"
	VerbFollowUp       Verb = "follow-up"
	VerbImport         Verb = "import"
	VerbTrack          Verb = "track"
)

type RunKind string

const (
	RunKindAgent    RunKind = "agent"
	RunKindFollowUp RunKind = "follow_up"
)

type RepoPhase string

const (
	RepoPhaseUnknown RepoPhase = ""
	RepoCloning      RepoPhase = "cloning"
	RepoReady        RepoPhase = "ready"
	RepoRefused      RepoPhase = "refused"
)

type RefusalKind string

const (
	RefusalUnknown       RefusalKind = ""
	RefusalMergeSettings RefusalKind = "merge_settings"
	RefusalDefaultBranch RefusalKind = "default_branch"
	RefusalClone         RefusalKind = "clone"
	RefusalSettingsParse RefusalKind = "settings_parse"
)

type GlyphWord string

const (
	GlyphUnknown   GlyphWord = ""
	GlyphFailed    GlyphWord = "failed"
	GlyphAttention GlyphWord = "attention"
	GlyphRunning   GlyphWord = "running"
	GlyphPending   GlyphWord = "pending"
	GlyphChecking  GlyphWord = "checking"
	GlyphReady     GlyphWord = "ready"
	GlyphBlocked   GlyphWord = "blocked"
	GlyphDone      GlyphWord = "done"
)

var (
	allVerbs = []Verb{
		VerbLaunch, VerbKill, VerbReRun, VerbRetryPush, VerbRemoveWorktree, VerbCancel,
		VerbRefresh, VerbAbort, VerbFollowUp, VerbImport, VerbTrack,
	}
	allRunKinds     = []RunKind{RunKindAgent, RunKindFollowUp}
	allRepoPhases   = []RepoPhase{RepoCloning, RepoReady, RepoRefused}
	allRefusalKinds = []RefusalKind{RefusalMergeSettings, RefusalDefaultBranch, RefusalClone, RefusalSettingsParse}
)

func AllVerbs() []Verb { return slices.Clone(allVerbs) }

func AllRunKinds() []RunKind { return slices.Clone(allRunKinds) }

func AllRefusalKinds() []RefusalKind { return slices.Clone(allRefusalKinds) }

func ParseVerb(s string) (Verb, error) { return parseMember("verb", s, allVerbs) }

func ParseRunKind(s string) (RunKind, error) { return parseMember("run kind", s, allRunKinds) }

func ParseRepoPhase(s string) (RepoPhase, error) { return parseMember("repo phase", s, allRepoPhases) }

func ParseRefusalKind(s string) (RefusalKind, error) {
	return parseMember("refusal kind", s, allRefusalKinds)
}

func (v Verb) IsRowVerb() bool {
	switch v {
	case VerbKill, VerbReRun, VerbRetryPush, VerbRemoveWorktree, VerbCancel,
		VerbRefresh, VerbAbort, VerbFollowUp:
		return true
	case VerbUnknown, VerbLaunch, VerbImport, VerbTrack:
		return false
	}
	return false
}

func parseMember[T ~string](name, s string, set []T) (T, error) {
	if v := T(s); slices.Contains(set, v) {
		return v, nil
	}
	var zero T
	return zero, fmt.Errorf("unknown %s %q", name, s)
}
