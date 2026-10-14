package plan_test

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestParseVerbAcceptsEveryVerb(t *testing.T) {
	t.Parallel()

	for _, want := range []plan.Verb{
		plan.VerbLaunch, plan.VerbKill, plan.VerbReRun, plan.VerbRetryPush, plan.VerbRemoveWorktree,
		plan.VerbCancel, plan.VerbRefresh, plan.VerbAbort, plan.VerbFollowUp, plan.VerbImport, plan.VerbTrack,
	} {
		got, err := plan.ParseVerb(string(want))
		if err != nil || got != want {
			t.Errorf("ParseVerb(%q) = %q, %v, want %q", want, got, err, want)
		}
	}
}

func TestParseRejectsAValueOutsideTheSet(t *testing.T) {
	t.Parallel()

	if _, err := plan.ParseVerb("bogus"); err == nil {
		t.Error(`ParseVerb("bogus") error = nil, want an error`)
	}
	if _, err := plan.ParseVerb(""); err == nil {
		t.Error(`ParseVerb("") error = nil, want an error: an empty value is VerbUnknown, not a verb`)
	}
	if _, err := plan.ParseRunKind("review"); err == nil {
		t.Error(`ParseRunKind("review") error = nil, want an error`)
	}
	if _, err := plan.ParseRepoPhase("gone"); err == nil {
		t.Error(`ParseRepoPhase("gone") error = nil, want an error`)
	}
	if _, err := plan.ParseRefusalKind("merge"); err == nil {
		t.Error(`ParseRefusalKind("merge") error = nil, want an error`)
	}
}

func TestParseRoundTripsTheOtherSets(t *testing.T) {
	t.Parallel()

	for _, want := range []plan.RunKind{plan.RunKindAgent, plan.RunKindFollowUp} {
		if got, err := plan.ParseRunKind(string(want)); err != nil || got != want {
			t.Errorf("ParseRunKind(%q) = %q, %v, want %q", want, got, err, want)
		}
	}
	for _, want := range []plan.RepoPhase{plan.RepoCloning, plan.RepoReady, plan.RepoRefused} {
		if got, err := plan.ParseRepoPhase(string(want)); err != nil || got != want {
			t.Errorf("ParseRepoPhase(%q) = %q, %v, want %q", want, got, err, want)
		}
	}
	for _, want := range []plan.RefusalKind{
		plan.RefusalMergeSettings, plan.RefusalDefaultBranch, plan.RefusalClone, plan.RefusalSettingsParse,
	} {
		if got, err := plan.ParseRefusalKind(string(want)); err != nil || got != want {
			t.Errorf("ParseRefusalKind(%q) = %q, %v, want %q", want, got, err, want)
		}
	}
}

func TestIsRowVerbExcludesLaunchImportAndTrack(t *testing.T) {
	t.Parallel()

	for verb, want := range map[plan.Verb]bool{
		plan.VerbKill: true, plan.VerbReRun: true, plan.VerbRetryPush: true, plan.VerbRemoveWorktree: true,
		plan.VerbCancel: true, plan.VerbRefresh: true, plan.VerbAbort: true, plan.VerbFollowUp: true,
		plan.VerbLaunch: false, plan.VerbImport: false, plan.VerbTrack: false, plan.VerbUnknown: false,
	} {
		if got := verb.IsRowVerb(); got != want {
			t.Errorf("%q.IsRowVerb() = %t, want %t", verb, got, want)
		}
	}
}
