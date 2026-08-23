package verdict_test

import (
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/verdict"
)

// supportAppPredicate transcribes support-app's real predicate (docs/designs/command-centre-design.md § 8,
// "re-verified"): five plain checks plus a three-way Linear any_of.
func supportAppPredicate() verdict.Predicate {
	return verdict.Predicate{AllOf: []verdict.Predicate{
		{Success: "Lint"},
		{Success: "Typecheck"},
		{Success: "Tests"},
		{Success: "Generated files"},
		{Success: "GraphQL production compatibility"},
		{AnyOf: []verdict.Predicate{
			{Success: "verify / Linear issue is linked"},
			{Success: "verify-linear-issue / Linear issue is linked"},
			{Success: "verify-linear-issue / Linear Issue Linked"},
		}},
	}}
}

func supportAppGreenChecks() map[string]verdict.CheckState {
	return map[string]verdict.CheckState{
		"Lint":                             verdict.Success,
		"Typecheck":                        verdict.Success,
		"Tests":                            verdict.Success,
		"Generated files":                  verdict.Success,
		"GraphQL production compatibility": verdict.Success,
		"verify / Linear issue is linked":  verdict.Success,
	}
}

// servicesPredicate transcribes services' real predicate (docs/designs/command-centre-design.md § 8): six
// plain checks, a three-way deployment any_of whose third arm is itself an all_of pairing a
// success with a skip, a four-way Linear any_of whose first arm is the dependabot author escape
// hatch, and the path-filtered absent_ok lint check.
func servicesPredicate() verdict.Predicate {
	return verdict.Predicate{AllOf: []verdict.Predicate{
		{Success: "Lint"},
		{Success: "Typecheck"},
		{Success: "Unit Test"},
		{Success: "Generated files up-to-date"},
		{Success: "Integration Tests Passed"},
		{Success: "Local Integration Tests Passed"},
		{AnyOf: []verdict.Predicate{
			{Success: "Deploy / Deploy SST Stage"},
			{Success: "Deploy / Deploy PR Stage"},
			{AllOf: []verdict.Predicate{{Success: "Evaluate"}, {Skipped: "Deploy"}}},
		}},
		{AnyOf: []verdict.Predicate{
			{Author: "dependabot[bot]"},
			{Success: "verify / Linear issue is linked"},
			{Success: "verify-linear-issue / Linear issue is linked"},
			{Success: "verify-linear-issue / Linear Issue Linked"},
		}},
		{AbsentOK: "Lint GitHub Actions / Lint"},
	}}
}

// servicesGreenChecks is a steady-state green rollup: the deployment arm resolves via
// "Deploy / Deploy SST Stage" rather than the skipped-Evaluate arm, the Linear arm via a real
// check (not the dependabot escape hatch), and "Lint GitHub Actions / Lint" is absent -- the
// path-filtered job never triggered for this diff.
func servicesGreenChecks() map[string]verdict.CheckState {
	return map[string]verdict.CheckState{
		"Lint":                            verdict.Success,
		"Typecheck":                       verdict.Success,
		"Unit Test":                       verdict.Success,
		"Generated files up-to-date":      verdict.Success,
		"Integration Tests Passed":        verdict.Success,
		"Local Integration Tests Passed":  verdict.Success,
		"Deploy / Deploy SST Stage":       verdict.Success,
		"verify / Linear issue is linked": verdict.Success,
	}
}

// waitedInput anchors PushedAt so BoundedWait has already elapsed against Now -- the steady
// state for a repo whose absent_ok check has had every chance to appear and hasn't.
func waitedInput() (pushedAt, now time.Time) {
	pushedAt = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return pushedAt, pushedAt.Add(verdict.BoundedWait)
}

// freshInput anchors PushedAt so BoundedWait has not elapsed -- a just-pushed commit.
func freshInput() (pushedAt, now time.Time) {
	pushedAt = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return pushedAt, pushedAt.Add(time.Minute)
}

func TestEvaluateSupportApp(t *testing.T) {
	t.Parallel()

	pushedAt, now := freshInput()
	base := verdict.Input{
		HeadOidMatch: true, ConfigHashOK: true, PushedAt: pushedAt, Now: now,
	}

	tests := []struct {
		name   string
		mutate func(verdict.Input) verdict.Input
		want   verdict.Verdict
	}{
		{
			name:   "all green derives review me",
			mutate: func(in verdict.Input) verdict.Input { in.Checks = supportAppGreenChecks(); return in },
			want:   verdict.ReviewMe,
		},
		{
			name: "one gating check red derives needs you",
			mutate: func(in verdict.Input) verdict.Input {
				checks := supportAppGreenChecks()
				checks["Typecheck"] = verdict.Failure
				in.Checks = checks
				return in
			},
			want: verdict.NeedsYou,
		},
		{
			name: "a green rollup at a different head SHA is not green",
			mutate: func(in verdict.Input) verdict.Input {
				in.Checks = supportAppGreenChecks()
				in.HeadOidMatch = false
				return in
			},
			want: verdict.Checking,
		},
		{
			name:   "an empty rollup is not green",
			mutate: func(in verdict.Input) verdict.Input { in.Checks = map[string]verdict.CheckState{}; return in },
			want:   verdict.Checking,
		},
		{
			name: "the Linear any_of resolves on its second arm alone",
			mutate: func(in verdict.Input) verdict.Input {
				checks := supportAppGreenChecks()
				delete(checks, "verify / Linear issue is linked")
				checks["verify-linear-issue / Linear issue is linked"] = verdict.Success
				in.Checks = checks
				return in
			},
			want: verdict.ReviewMe,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := verdict.Evaluate(supportAppPredicate(), tt.mutate(base))
			if got.Verdict != tt.want {
				t.Errorf("verdict = %v (%s), want %v", got.Verdict, got.Reason, tt.want)
			}
		})
	}
}

func TestEvaluateServices(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   verdict.Input
		want verdict.Verdict
	}{
		{
			name: "all green derives review me",
			in: func() verdict.Input {
				pushedAt, now := waitedInput()
				return verdict.Input{
					Checks: servicesGreenChecks(), HeadOidMatch: true, ConfigHashOK: true,
					PushedAt: pushedAt, Now: now, AuthorLogin: "a-real-human",
				}
			}(),
			want: verdict.ReviewMe,
		},
		{
			name: "the skipped-Deploy arm resolves without a matching Deploy SST or PR Stage check-run",
			in: func() verdict.Input {
				pushedAt, now := waitedInput()
				checks := servicesGreenChecks()
				delete(checks, "Deploy / Deploy SST Stage")
				checks["Evaluate"] = verdict.Success
				checks["Deploy"] = verdict.Skipped
				return verdict.Input{
					Checks: checks, HeadOidMatch: true, ConfigHashOK: true,
					PushedAt: pushedAt, Now: now, AuthorLogin: "a-real-human",
				}
			}(),
			want: verdict.ReviewMe,
		},
		{
			name: "the dependabot arm resolves without a matching Linear check-run",
			in: func() verdict.Input {
				pushedAt, now := waitedInput()
				checks := servicesGreenChecks()
				delete(checks, "verify / Linear issue is linked")
				return verdict.Input{
					Checks: checks, HeadOidMatch: true, ConfigHashOK: true,
					PushedAt: pushedAt, Now: now, AuthorLogin: "dependabot[bot]",
				}
			}(),
			want: verdict.ReviewMe,
		},
		{
			name: "an absent absent_ok check is checking while young",
			in: func() verdict.Input {
				pushedAt, now := freshInput()
				return verdict.Input{
					Checks: servicesGreenChecks(), HeadOidMatch: true, ConfigHashOK: true,
					PushedAt: pushedAt, Now: now, AuthorLogin: "a-real-human",
				}
			}(),
			want: verdict.Checking,
		},
		{
			name: "an absent absent_ok check is review me once the wait elapses",
			in: func() verdict.Input {
				pushedAt, now := waitedInput()
				return verdict.Input{
					Checks: servicesGreenChecks(), HeadOidMatch: true, ConfigHashOK: true,
					PushedAt: pushedAt, Now: now, AuthorLogin: "a-real-human",
				}
			}(),
			want: verdict.ReviewMe,
		},
		{
			name: "a present but failed absent_ok check is needs you regardless of the wait",
			in: func() verdict.Input {
				pushedAt, now := freshInput()
				checks := servicesGreenChecks()
				checks["Lint GitHub Actions / Lint"] = verdict.Failure
				return verdict.Input{
					Checks: checks, HeadOidMatch: true, ConfigHashOK: true,
					PushedAt: pushedAt, Now: now, AuthorLogin: "a-real-human",
				}
			}(),
			want: verdict.NeedsYou,
		},
		{
			name: "a green result is suppressed while the mergify hash is stale",
			in: func() verdict.Input {
				pushedAt, now := waitedInput()
				return verdict.Input{
					Checks: servicesGreenChecks(), HeadOidMatch: true, ConfigHashOK: false,
					PushedAt: pushedAt, Now: now, AuthorLogin: "a-real-human",
				}
			}(),
			want: verdict.Checking,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := verdict.Evaluate(servicesPredicate(), tt.in)
			if got.Verdict != tt.want {
				t.Errorf("verdict = %v (%s), want %v", got.Verdict, got.Reason, tt.want)
			}
		})
	}
}

func TestEvaluateGrammar(t *testing.T) {
	t.Parallel()

	present := map[string]verdict.CheckState{"X": verdict.Success}
	base := verdict.Input{Checks: present, HeadOidMatch: true, ConfigHashOK: true}

	tests := []struct {
		name string
		p    verdict.Predicate
		in   verdict.Input
		want verdict.Verdict
	}{
		{
			name: "not inverts a green leaf to red",
			p:    verdict.Predicate{Not: &verdict.Predicate{Success: "X"}},
			in:   base,
			want: verdict.NeedsYou,
		},
		{
			name: "not inverts a red leaf to green",
			p:    verdict.Predicate{Not: &verdict.Predicate{Success: "Y"}},
			in: verdict.Input{
				Checks: map[string]verdict.CheckState{"Y": verdict.Failure}, HeadOidMatch: true, ConfigHashOK: true,
			},
			want: verdict.ReviewMe,
		},
		{
			name: "a skipped leaf whose check actually succeeded is needs you, not review me",
			p:    verdict.Predicate{Skipped: "X"},
			in:   base,
			want: verdict.NeedsYou,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := verdict.Evaluate(tt.p, tt.in)
			if got.Verdict != tt.want {
				t.Errorf("verdict = %v (%s), want %v", got.Verdict, got.Reason, tt.want)
			}
		})
	}
}

func TestPredicateIsZero(t *testing.T) {
	t.Parallel()

	if !(verdict.Predicate{}).IsZero() {
		t.Error("zero-value Predicate is not reported as zero")
	}
	if (verdict.Predicate{Success: "X"}).IsZero() {
		t.Error("a leaf Predicate is reported as zero")
	}
}

// TestBoundedWaitOnlyCountsSuccessfulTicks is the pure half of the AC: Evaluate takes whatever
// Now the caller derives, so a caller that (correctly) derives Now only from ticks whose observe
// phase succeeded -- never from wall time -- must see the wait hold no matter how long a real
// outage actually lasted. internal/cc's loop test covers the other half: that its own Now really
// is built that way.
func TestBoundedWaitOnlyCountsSuccessfulTicks(t *testing.T) {
	t.Parallel()

	pushedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p := verdict.Predicate{Success: "CI"}
	in := verdict.Input{
		PushedAt: pushedAt, Checks: map[string]verdict.CheckState{}, HeadOidMatch: true,
	}

	// A forced-failure sequence: however long the real outage ran, zero ticks observed
	// successfully means the caller's derived Now has not moved past PushedAt at all.
	in.Now = pushedAt
	if got := verdict.Evaluate(p, in).Verdict; got != verdict.Checking {
		t.Fatalf("verdict = %v after zero successful ticks, want checking", got)
	}

	// Once observe succeeds again, ticks accumulate for real and the wait can elapse.
	in.Now = pushedAt.Add(verdict.BoundedWait)
	if got := verdict.Evaluate(p, in).Verdict; got != verdict.NeedsYou {
		t.Fatalf("verdict = %v once the wait elapses over successful ticks, want needs_you", got)
	}
}

func TestVerdictString(t *testing.T) {
	t.Parallel()

	if verdict.ReviewMe.String() != "review_me" || verdict.NeedsYou.String() != "needs_you" ||
		verdict.Checking.String() != "checking" {
		t.Errorf("verdicts render as %q, %q, %q", verdict.ReviewMe, verdict.NeedsYou, verdict.Checking)
	}
}
