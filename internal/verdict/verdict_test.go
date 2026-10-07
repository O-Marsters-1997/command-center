package verdict_test

import (
	"slices"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/verdict"
)

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

func waitedInput() (pushedAt, now time.Time) {
	pushedAt = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return pushedAt, pushedAt.Add(verdict.BoundedWait)
}

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

func TestBoundedWaitOnlyCountsSuccessfulTicks(t *testing.T) {
	t.Parallel()

	pushedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p := verdict.Predicate{Success: "CI"}
	in := verdict.Input{
		PushedAt: pushedAt, Checks: map[string]verdict.CheckState{}, HeadOidMatch: true,
	}

	in.Now = pushedAt
	if got := verdict.Evaluate(p, in).Verdict; got != verdict.Checking {
		t.Fatalf("verdict = %v after zero successful ticks, want checking", got)
	}

	in.Now = pushedAt.Add(verdict.BoundedWait)
	if got := verdict.Evaluate(p, in).Verdict; got != verdict.NeedsYou {
		t.Fatalf("verdict = %v once the wait elapses over successful ticks, want needs_you", got)
	}
}

func TestNeedsYouNamesTheRedLeaf(t *testing.T) {
	t.Parallel()

	pushedAt, now := freshInput()
	p := verdict.Predicate{Success: "CI"}

	got := verdict.Evaluate(p, verdict.Input{
		Checks:       map[string]verdict.CheckState{"CI": verdict.Failure},
		HeadOidMatch: true, ConfigHashOK: true, PushedAt: pushedAt, Now: now,
	})
	if got.Verdict != verdict.NeedsYou {
		t.Fatalf("verdict = %v, want needs_you", got.Verdict)
	}
	if want := []string{"CI"}; !slices.Equal(got.RedLeaves, want) {
		t.Errorf("RedLeaves = %v, want %v", got.RedLeaves, want)
	}

	waitedPushedAt, waitedNow := waitedInput()
	elapsed := verdict.Evaluate(p, verdict.Input{
		Checks: map[string]verdict.CheckState{}, HeadOidMatch: true, ConfigHashOK: true,
		PushedAt: waitedPushedAt, Now: waitedNow,
	})
	if elapsed.Verdict != verdict.NeedsYou {
		t.Fatalf("verdict = %v, want needs_you", elapsed.Verdict)
	}
	if len(elapsed.RedLeaves) != 0 {
		t.Errorf("RedLeaves = %v, want none: no check ever resolved red, the wait just elapsed", elapsed.RedLeaves)
	}
}

func TestAllOfNamesEveryRedLeaf(t *testing.T) {
	t.Parallel()

	pushedAt, now := freshInput()
	p := verdict.Predicate{AllOf: []verdict.Predicate{{Success: "Lint"}, {Success: "Tests"}}}

	got := verdict.Evaluate(p, verdict.Input{
		Checks:       map[string]verdict.CheckState{"Lint": verdict.Failure, "Tests": verdict.Failure},
		HeadOidMatch: true, ConfigHashOK: true, PushedAt: pushedAt, Now: now,
	})
	if got.Verdict != verdict.NeedsYou {
		t.Fatalf("verdict = %v, want needs_you", got.Verdict)
	}
	if want := []string{"Lint", "Tests"}; !slices.Equal(got.RedLeaves, want) {
		t.Errorf("RedLeaves = %v, want %v: both required checks failed", got.RedLeaves, want)
	}
}

// compatPredicate is a two-check predicate standing in for a real repo's, one leaf named as the
// cross-repo compat check.
func compatPredicate() verdict.Predicate {
	return verdict.Predicate{AllOf: []verdict.Predicate{
		{Success: "GraphQL production compatibility"},
		{Success: "Tests"},
	}}
}

const compatCheckName = "GraphQL production compatibility"

func TestEvaluateWaitingOnProducerDeploy(t *testing.T) {
	t.Parallel()

	pushedAt, now := freshInput()
	base := verdict.Input{
		HeadOidMatch: true, ConfigHashOK: true, PushedAt: pushedAt, Now: now, CompatCheck: compatCheckName,
	}

	tests := []struct {
		name string
		in   verdict.Input
		want verdict.Verdict
	}{
		{
			name: "the compat check red alone derives waiting on producer deploy",
			in: func() verdict.Input {
				in := base
				in.Checks = map[string]verdict.CheckState{
					compatCheckName: verdict.Failure, "Tests": verdict.Success,
				}
				return in
			}(),
			want: verdict.WaitingOnProducerDeploy,
		},
		{
			name: "the compat check red and another required check red derives needs you",
			in: func() verdict.Input {
				in := base
				in.Checks = map[string]verdict.CheckState{
					compatCheckName: verdict.Failure, "Tests": verdict.Failure,
				}
				return in
			}(),
			want: verdict.NeedsYou,
		},
		{
			name: "the compat check red while another required check is pending derives checking",
			in: func() verdict.Input {
				in := base
				in.Checks = map[string]verdict.CheckState{compatCheckName: verdict.Failure}
				return in
			}(),
			want: verdict.Checking,
		},
		{
			name: "an unconfigured compat check never derives waiting on producer deploy",
			in: func() verdict.Input {
				in := base
				in.CompatCheck = ""
				in.Checks = map[string]verdict.CheckState{
					compatCheckName: verdict.Failure, "Tests": verdict.Success,
				}
				return in
			}(),
			want: verdict.NeedsYou,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := verdict.Evaluate(compatPredicate(), tt.in)
			if got.Verdict != tt.want {
				t.Errorf("verdict = %v (%s), want %v", got.Verdict, got.Reason, tt.want)
			}
		})
	}
}

func TestWaitingOnProducerDeploySurvivesTheBoundedWait(t *testing.T) {
	t.Parallel()

	pushedAt, _ := waitedInput()
	in := verdict.Input{
		HeadOidMatch: true, ConfigHashOK: true, PushedAt: pushedAt, CompatCheck: compatCheckName,
		Checks: map[string]verdict.CheckState{
			compatCheckName: verdict.Failure, "Tests": verdict.Success,
		},
		Now: pushedAt.Add(10 * verdict.BoundedWait),
	}

	got := verdict.Evaluate(compatPredicate(), in)
	if got.Verdict != verdict.WaitingOnProducerDeploy {
		t.Fatalf("verdict = %v (%s) ten bounded waits after the resolved red, want waiting_on_producer_deploy",
			got.Verdict, got.Reason)
	}
}

func TestEvaluateBaseMoved(t *testing.T) {
	t.Parallel()

	pushedAt, now := freshInput()
	greenBase := verdict.Input{
		Checks: supportAppGreenChecks(), HeadOidMatch: true, ConfigHashOK: true, PushedAt: pushedAt, Now: now,
	}

	tests := []struct {
		name string
		in   verdict.Input
		want verdict.Verdict
	}{
		{
			name: "a stacked base whose tip moved is base moved even with every check green",
			in:   func() verdict.Input { in := greenBase; in.StackedBase, in.BaseSHAMatch = true, false; return in }(),
			want: verdict.BaseMoved,
		},
		{
			name: "a stacked base whose tip moved is base moved even with a red check",
			in: func() verdict.Input {
				in := greenBase
				checks := supportAppGreenChecks()
				checks["Typecheck"] = verdict.Failure
				in.Checks = checks
				in.StackedBase, in.BaseSHAMatch = true, false
				return in
			}(),
			want: verdict.BaseMoved,
		},
		{
			name: "a stacked base whose tip has not moved evaluates normally",
			in:   func() verdict.Input { in := greenBase; in.StackedBase, in.BaseSHAMatch = true, true; return in }(),
			want: verdict.ReviewMe,
		},
		{
			name: "a root row never reads base moved however BaseSHAMatch reads",
			in:   func() verdict.Input { in := greenBase; in.StackedBase, in.BaseSHAMatch = false, false; return in }(),
			want: verdict.ReviewMe,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := verdict.Evaluate(supportAppPredicate(), tt.in)
			if got.Verdict != tt.want {
				t.Errorf("verdict = %v (%s), want %v", got.Verdict, got.Reason, tt.want)
			}
		})
	}
}
