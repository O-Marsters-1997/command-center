package plan_test

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestParseVerb(t *testing.T) {
	t.Parallel()

	for _, want := range []plan.Verb{
		plan.VerbLaunch, plan.VerbKill, plan.VerbReRun, plan.VerbRetryPush, plan.VerbRemoveWorktree,
		plan.VerbCancel, plan.VerbRefresh, plan.VerbAbort, plan.VerbFollowUp, plan.VerbImport, plan.VerbTrack,
	} {
		t.Run(string(want), func(t *testing.T) {
			t.Parallel()
			got, err := plan.ParseVerb(string(want))
			if err != nil || got != want {
				t.Errorf("ParseVerb(%q) = %q, %v, want %q, nil", want, got, err, want)
			}
		})
	}

	for _, in := range []string{"bogus", ""} {
		t.Run("rejects "+in, func(t *testing.T) {
			t.Parallel()
			if _, err := plan.ParseVerb(in); err == nil {
				t.Errorf("ParseVerb(%q) error = nil, want one", in)
			}
		})
	}
}

func TestParseRunKind(t *testing.T) {
	t.Parallel()

	for _, want := range []plan.RunKind{plan.RunKindAgent, plan.RunKindFollowUp, plan.RunKindCommand} {
		t.Run(string(want), func(t *testing.T) {
			t.Parallel()
			if got, err := plan.ParseRunKind(string(want)); err != nil || got != want {
				t.Errorf("ParseRunKind(%q) = %q, %v, want %q, nil", want, got, err, want)
			}
		})
	}

	t.Run("rejects review", func(t *testing.T) {
		t.Parallel()
		if _, err := plan.ParseRunKind("review"); err == nil {
			t.Error(`ParseRunKind("review") error = nil, want one`)
		}
	})
}

func TestParseRepoPhase(t *testing.T) {
	t.Parallel()

	for _, want := range []plan.RepoPhase{plan.RepoCloning, plan.RepoReady, plan.RepoRefused} {
		t.Run(string(want), func(t *testing.T) {
			t.Parallel()
			if got, err := plan.ParseRepoPhase(string(want)); err != nil || got != want {
				t.Errorf("ParseRepoPhase(%q) = %q, %v, want %q, nil", want, got, err, want)
			}
		})
	}

	t.Run("rejects gone", func(t *testing.T) {
		t.Parallel()
		if _, err := plan.ParseRepoPhase("gone"); err == nil {
			t.Error(`ParseRepoPhase("gone") error = nil, want one`)
		}
	})
}

func TestParseRefusalKind(t *testing.T) {
	t.Parallel()

	for _, want := range []plan.RefusalKind{
		plan.RefusalMergeSettings, plan.RefusalDefaultBranch, plan.RefusalClone, plan.RefusalSettingsParse,
	} {
		t.Run(string(want), func(t *testing.T) {
			t.Parallel()
			if got, err := plan.ParseRefusalKind(string(want)); err != nil || got != want {
				t.Errorf("ParseRefusalKind(%q) = %q, %v, want %q, nil", want, got, err, want)
			}
		})
	}

	t.Run("rejects merge", func(t *testing.T) {
		t.Parallel()
		if _, err := plan.ParseRefusalKind("merge"); err == nil {
			t.Error(`ParseRefusalKind("merge") error = nil, want one`)
		}
	})
}

func TestVerb_IsRowVerb(t *testing.T) {
	t.Parallel()

	for verb, want := range map[plan.Verb]bool{
		plan.VerbKill: true, plan.VerbReRun: true, plan.VerbRetryPush: true, plan.VerbRemoveWorktree: true,
		plan.VerbCancel: true, plan.VerbRefresh: true, plan.VerbAbort: true, plan.VerbFollowUp: true,
		plan.VerbLaunch: false, plan.VerbImport: false, plan.VerbTrack: false, plan.VerbUnknown: false,
	} {
		t.Run(string(verb), func(t *testing.T) {
			t.Parallel()
			if got := verb.IsRowVerb(); got != want {
				t.Errorf("%q.IsRowVerb() = %t, want %t", verb, got, want)
			}
		})
	}
}

func TestRunKind_Predicates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind                                             plan.RunKind
		steerable, startsSession, takesSystem, isCommand bool
	}{
		{kind: plan.RunKindAgent, steerable: true, startsSession: true, takesSystem: true},
		{kind: plan.RunKindFollowUp, steerable: true, startsSession: true},
		{kind: plan.RunKindResolve, startsSession: true, takesSystem: true},
		{kind: plan.RunKindCommand, isCommand: true},
		{kind: plan.RunKindVerify, isCommand: true},
		{kind: plan.RunKindUnknown},
	}
	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			t.Parallel()
			if got := tt.kind.Steerable(); got != tt.steerable {
				t.Errorf("%q.Steerable() = %v, want %v", tt.kind, got, tt.steerable)
			}
			if got := tt.kind.StartsSession(); got != tt.startsSession {
				t.Errorf("%q.StartsSession() = %v, want %v", tt.kind, got, tt.startsSession)
			}
			if got := tt.kind.TakesSystemPrompt(); got != tt.takesSystem {
				t.Errorf("%q.TakesSystemPrompt() = %v, want %v", tt.kind, got, tt.takesSystem)
			}
			if got := tt.kind.IsCommand(); got != tt.isCommand {
				t.Errorf("%q.IsCommand() = %v, want %v", tt.kind, got, tt.isCommand)
			}
		})
	}
}
