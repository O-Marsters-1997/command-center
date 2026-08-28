package plan_test

import (
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		unlock     plan.Unlock
		authorised bool
		want       plan.State
		wantReason plan.Reason
	}{
		{
			name:       "locked and unauthorised derives blocked",
			unlock:     plan.Unlock{Unlocked: false, Reason: "waiting on sandbox://CC-1"},
			want:       plan.Blocked,
			wantReason: "waiting on sandbox://CC-1",
		},
		{
			name:       "unlocked and unauthorised derives ready",
			unlock:     plan.Unlock{Unlocked: true, BaseBranch: "main", Reason: "no blockers"},
			want:       plan.Ready,
			wantReason: "no blockers",
		},
		{
			name:       "unlocked and authorised derives queued waiting for a slot",
			unlock:     plan.Unlock{Unlocked: true, BaseBranch: "main", Reason: "no blockers"},
			authorised: true,
			want:       plan.Queued,
			wantReason: "waiting for a slot",
		},
		{
			name:       "locked and authorised derives queued naming the blocker",
			unlock:     plan.Unlock{Reason: "blocked by sandbox://CC-1", Blocking: []string{"sandbox://CC-1"}},
			authorised: true,
			want:       plan.Queued,
			wantReason: "waiting on sandbox://CC-1's PR",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			state, reason := plan.Status(plan.Facts{Unlock: tt.unlock, Authorised: tt.authorised})
			if state != tt.want {
				t.Errorf("state = %v, want %v", state, tt.want)
			}
			if reason != tt.wantReason {
				t.Errorf("reason = %q, want %q", reason, tt.wantReason)
			}
		})
	}
}

func TestStateString(t *testing.T) {
	t.Parallel()

	if plan.Blocked.String() != "blocked" || plan.Ready.String() != "ready" || plan.Queued.String() != "queued" {
		t.Errorf("states render as %q, %q and %q", plan.Blocked, plan.Ready, plan.Queued)
	}
	if plan.Checking.String() != "checking" || plan.NeedsYou.String() != "needs_you" ||
		plan.PushFailed.String() != "push_failed" {
		t.Errorf("states render as %q, %q and %q", plan.Checking, plan.NeedsYou, plan.PushFailed)
	}
	if plan.ReviewMe.String() != "review_me" {
		t.Errorf("state renders as %q, want review_me", plan.ReviewMe)
	}
	if plan.PRMerged.String() != "merged" || plan.PRClosedUnmerged.String() != "pr_closed_unmerged" ||
		plan.BaseGone.String() != "base_gone" {
		t.Errorf("states render as %q, %q and %q", plan.PRMerged, plan.PRClosedUnmerged, plan.BaseGone)
	}
	if plan.Cancelled.String() != "cancelled" {
		t.Errorf("state renders as %q, want cancelled", plan.Cancelled)
	}
	if plan.BaseMoved.String() != "base_moved" {
		t.Errorf("state renders as %q, want base_moved", plan.BaseMoved)
	}
	if plan.RefreshConflicted.String() != "refresh_conflicted" {
		t.Errorf("state renders as %q, want refresh_conflicted", plan.RefreshConflicted)
	}
	if plan.WaitingOnProducerDeploy.String() != "waiting_on_producer_deploy" {
		t.Errorf("state renders as %q, want waiting_on_producer_deploy", plan.WaitingOnProducerDeploy)
	}
}

func TestStatusDerivesCancelledForAMemberWithNoRun(t *testing.T) {
	t.Parallel()

	state, reason := plan.Status(plan.Facts{
		Unlock:          plan.Unlock{Unlocked: true, BaseBranch: "main", Reason: "no blockers"},
		Authorised:      true,
		CancelledMember: true,
	})
	if state != plan.Cancelled {
		t.Errorf("state = %v, want cancelled", state)
	}
	if reason == "" {
		t.Error("reason is empty")
	}
}

func TestStatusNeverDerivesCancelledOnceItHasRun(t *testing.T) {
	t.Parallel()

	state, _ := plan.Status(plan.Facts{
		Unlock:          plan.Unlock{Unlocked: true, BaseBranch: "main", Reason: "no blockers"},
		LatestRun:       &plan.RunFact{Alive: true},
		CancelledMember: true,
	})
	if state != plan.Running {
		t.Errorf("state = %v, want running: a row that ever ran is never cancelled", state)
	}
}

func TestStatusWithLatestRun(t *testing.T) {
	t.Parallel()

	unlocked := plan.Unlock{Unlocked: true, BaseBranch: "main", Reason: "no blockers"}

	tests := []struct {
		name       string
		latestRun  *plan.RunFact
		wantState  plan.State
		reasonHas  string
		reasonHasA string // second required substring, for the failed-with-log-path case
	}{
		{
			name:      "an alive run derives running regardless of unlock or authorisation",
			latestRun: &plan.RunFact{Alive: true},
			wantState: plan.Running,
		},
		{
			name: "a dead run with no commits derives failed, naming the log path",
			latestRun: &plan.RunFact{
				Alive: false, HasOutcome: true, Outcome: plan.OutcomeFailed, LogPath: "/state/runs/7.jsonl",
			},
			wantState:  plan.Failed,
			reasonHas:  "no commits",
			reasonHasA: "/state/runs/7.jsonl",
		},
		{
			name: "a dead run with commits derives push pending",
			latestRun: &plan.RunFact{
				Alive: false, HasOutcome: true, Outcome: plan.OutcomePush, LogPath: "/state/runs/8.jsonl",
			},
			wantState: plan.PushPending,
		},
		{
			name: "a failed cut derives cut failed",
			latestRun: &plan.RunFact{
				Alive: false, HasOutcome: true, Outcome: plan.OutcomeCutFailed,
			},
			wantState: plan.CutFailed,
		},
		{
			name: "a refused push derives needs you, naming the path",
			latestRun: &plan.RunFact{
				Alive: false, HasOutcome: true, Outcome: plan.OutcomePush,
				PushRefused: true, PushRefusedPath: ".github/workflows/ci.yml",
			},
			wantState: plan.NeedsYou,
			reasonHas: ".github/workflows/ci.yml",
		},
		{
			name: "a push or PR-create failure derives push failed",
			latestRun: &plan.RunFact{
				Alive: false, HasOutcome: true, Outcome: plan.OutcomePush, PushFailed: true,
			},
			wantState: plan.PushFailed,
		},
		{
			name: "an open PR with no verdict yet derives checking",
			latestRun: &plan.RunFact{
				Alive: false, HasOutcome: true, Outcome: plan.OutcomePush, PROpen: true,
			},
			wantState: plan.Checking,
			reasonHas: "no verdict yet",
		},
		{
			name: "an open PR with a checking verdict derives checking, naming the verdict's reason",
			latestRun: &plan.RunFact{
				Alive: false, HasOutcome: true, Outcome: plan.OutcomePush, PROpen: true,
				VerdictReason: "check config changed",
			},
			wantState: plan.Checking,
			reasonHas: "check config changed",
		},
		{
			name: "an open PR with a review-me verdict derives review me",
			latestRun: &plan.RunFact{
				Alive: false, HasOutcome: true, Outcome: plan.OutcomePush, PROpen: true,
				VerdictReviewMe: true, VerdictReason: "every required check passed",
			},
			wantState: plan.ReviewMe,
			reasonHas: "every required check passed",
		},
		{
			name: "an open PR with a needs-you verdict derives needs you over push facts alone",
			latestRun: &plan.RunFact{
				Alive: false, HasOutcome: true, Outcome: plan.OutcomePush, PROpen: true,
				VerdictNeedsYou: true, VerdictReason: "a required check failed",
			},
			wantState: plan.NeedsYou,
			reasonHas: "a required check failed",
		},
		{
			name: "commits with no push attempt yet still derives push pending",
			latestRun: &plan.RunFact{
				Alive: false, HasOutcome: true, Outcome: plan.OutcomePush,
			},
			wantState: plan.PushPending,
		},
		{
			name: "the pull request having merged outranks an open-PR verdict",
			latestRun: &plan.RunFact{
				Alive: false, HasOutcome: true, Outcome: plan.OutcomePush, PROpen: false, PRMerged: true,
				VerdictReviewMe: true, VerdictReason: "every required check passed",
			},
			wantState: plan.PRMerged,
		},
		{
			name: "the pull request having closed unmerged outranks a would-be checking state",
			latestRun: &plan.RunFact{
				Alive: false, HasOutcome: true, Outcome: plan.OutcomePush, PRClosedUnmerged: true,
			},
			wantState: plan.PRClosedUnmerged,
		},
		{
			name: "a waiting-on-producer-deploy verdict derives waiting on producer deploy",
			latestRun: &plan.RunFact{
				Alive: false, HasOutcome: true, Outcome: plan.OutcomePush, PROpen: true,
				VerdictWaitingOnProducer: true, VerdictReason: "every required check passed except the compat check",
			},
			wantState: plan.WaitingOnProducerDeploy,
			reasonHas: "except the compat check",
		},
		{
			name: "a base-moved verdict derives base moved even over a needs-you check reading",
			latestRun: &plan.RunFact{
				Alive: false, HasOutcome: true, Outcome: plan.OutcomePush, PROpen: true,
				VerdictBaseMoved: true, VerdictReason: "base moved: the parent advanced past what this branch was cut from",
			},
			wantState: plan.BaseMoved,
			reasonHas: "the parent advanced",
		},
		{
			name: "a refused fast-forward derives needs you naming the reason, outranking base moved",
			latestRun: &plan.RunFact{
				Alive: false, HasOutcome: true, Outcome: plan.OutcomePush, PROpen: true,
				VerdictBaseMoved: true, RefreshRefused: true,
				RefreshRefusedReason: "not a fast-forward: diverged from origin/cc-1",
			},
			wantState: plan.NeedsYou,
			reasonHas: "not a fast-forward",
		},
		{
			name: "an unresolved merge derives refresh conflicted over base moved and a refused fast-forward",
			latestRun: &plan.RunFact{
				Alive: false, HasOutcome: true, Outcome: plan.OutcomePush, PROpen: true,
				MidMerge: true, VerdictBaseMoved: true, RefreshRefused: true,
				RefreshRefusedReason: "not a fast-forward: diverged from origin/cc-1",
			},
			wantState: plan.RefreshConflicted,
			reasonHas: "mid-merge",
		},
		{
			name: "an unresolved merge outranks a push failure, whose re-run would spawn into it",
			latestRun: &plan.RunFact{
				Alive: false, HasOutcome: true, Outcome: plan.OutcomePush, MidMerge: true, PushFailed: true,
			},
			wantState: plan.RefreshConflicted,
		},
		{
			name: "a merged pull request outranks an unresolved merge in the worktree",
			latestRun: &plan.RunFact{
				Alive: false, HasOutcome: true, Outcome: plan.OutcomePush, PRMerged: true, MidMerge: true,
			},
			wantState: plan.PRMerged,
		},
		{
			name: "a branch that no longer merges into main derives conflicts with main, naming the branch",
			latestRun: &plan.RunFact{
				Alive: false, HasOutcome: true, Outcome: plan.OutcomePush, PROpen: true,
				ConflictsWithMain: true, ConflictsWithMainReason: "cc-9-example no longer merges cleanly into main",
			},
			wantState: plan.ConflictsWithMain,
			reasonHas: "no longer merges cleanly into main",
		},
		{
			name: "conflicts with main outranks a refused fast-forward and a base-moved verdict",
			latestRun: &plan.RunFact{
				Alive: false, HasOutcome: true, Outcome: plan.OutcomePush, PROpen: true,
				ConflictsWithMain: true, ConflictsWithMainReason: "cc-9-example no longer merges cleanly into main",
				VerdictBaseMoved: true, RefreshRefused: true,
				RefreshRefusedReason: "not a fast-forward: diverged from origin/cc-1",
			},
			wantState: plan.ConflictsWithMain,
		},
		{
			name: "an unresolved merge outranks conflicts with main, whose refresh would spawn into it",
			latestRun: &plan.RunFact{
				Alive: false, HasOutcome: true, Outcome: plan.OutcomePush, MidMerge: true, ConflictsWithMain: true,
			},
			wantState: plan.RefreshConflicted,
		},
		{
			name: "a merged pull request outranks a branch that no longer merges into main",
			latestRun: &plan.RunFact{
				Alive: false, HasOutcome: true, Outcome: plan.OutcomePush, PRMerged: true, ConflictsWithMain: true,
			},
			wantState: plan.PRMerged,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			state, reason := plan.Status(plan.Facts{Unlock: unlocked, Authorised: false, LatestRun: tt.latestRun})
			if state != tt.wantState {
				t.Errorf("state = %v, want %v (reason %q)", state, tt.wantState, reason)
			}
			if tt.reasonHas != "" && !strings.Contains(string(reason), tt.reasonHas) {
				t.Errorf("reason %q does not contain %q", reason, tt.reasonHas)
			}
			if tt.reasonHasA != "" && !strings.Contains(string(reason), tt.reasonHasA) {
				t.Errorf("reason %q does not contain the log path %q", reason, tt.reasonHasA)
			}
		})
	}
}

func TestStatusIgnoresANilLatestRun(t *testing.T) {
	t.Parallel()

	// The pre-Phase-3 2x2 stays reachable and untouched when there is no run yet.
	state, reason := plan.Status(plan.Facts{
		Unlock:     plan.Unlock{Unlocked: true, BaseBranch: "main", Reason: "no blockers"},
		Authorised: false,
	})
	if state != plan.Ready || reason != "no blockers" {
		t.Errorf("state = %v, reason = %q, want ready/\"no blockers\"", state, reason)
	}
}

// TestStatusDerivesBaseGoneOverAnythingElseOnceItHasRun covers inv. 19: a row that has ever run
// derives base_gone the moment its blocker's PR is closed unmerged, never blocked — and this
// outranks even a live run or a fully-resolved push, which the design's state diagram (§5)
// draws as transitioning into base_gone from any of them.
func TestStatusDerivesBaseGoneOverAnythingElseOnceItHasRun(t *testing.T) {
	t.Parallel()

	blockerClosed := plan.Unlock{
		Reason:        "blocked by sandbox://CC-1: cc-1's pull request was closed without merging",
		BlockerClosed: true,
	}

	tests := []struct {
		name      string
		latestRun *plan.RunFact
	}{
		{name: "a live run", latestRun: &plan.RunFact{Alive: true}},
		{name: "a failed run", latestRun: &plan.RunFact{HasOutcome: true, Outcome: plan.OutcomeFailed}},
		{name: "a run still waiting to push", latestRun: &plan.RunFact{HasOutcome: true, Outcome: plan.OutcomePush}},
		{name: "a run already checking", latestRun: &plan.RunFact{HasOutcome: true, Outcome: plan.OutcomePush, PROpen: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			state, reason := plan.Status(plan.Facts{Unlock: blockerClosed, Authorised: true, LatestRun: tt.latestRun})
			if state != plan.BaseGone {
				t.Errorf("state = %v, want base_gone", state)
			}
			if reason != blockerClosed.Reason {
				t.Errorf("reason = %q, want the unlock reason %q", reason, blockerClosed.Reason)
			}
		})
	}
}

// TestStatusNeverDerivesBaseGoneWithoutAPriorRun covers the other half of inv. 19: a member
// that never launched re-derives blocked or queued, exactly the pre-Phase-3 2x2, even when its
// blocker's PR was closed unmerged.
func TestStatusNeverDerivesBaseGoneWithoutAPriorRun(t *testing.T) {
	t.Parallel()

	blockerClosed := plan.Unlock{
		Reason: "blocked by sandbox://CC-1", BlockerClosed: true, Blocking: []string{"sandbox://CC-1"},
	}

	state, _ := plan.Status(plan.Facts{Unlock: blockerClosed, Authorised: false, LatestRun: nil})
	if state != plan.Blocked {
		t.Errorf("state = %v, want blocked: no run ever happened", state)
	}

	state, _ = plan.Status(plan.Facts{Unlock: blockerClosed, Authorised: true, LatestRun: nil})
	if state != plan.Queued {
		t.Errorf("state = %v, want queued: authorised but never launched", state)
	}
}

// TestStatusBlocksOnAConflictedBase covers the launch gate's own row: a base that already
// carries a conflict must say so, rather than leaving an authorised row queued on a slot that
// the tick will refuse every time it comes round.
func TestStatusBlocksOnAConflictedBase(t *testing.T) {
	t.Parallel()

	for _, authorised := range []bool{false, true} {
		state, reason := plan.Status(plan.Facts{
			Unlock:         plan.Unlock{Unlocked: true, BaseBranch: "cc-1-first", Reason: "every blocker has a pull request"},
			Authorised:     authorised,
			ConflictedBase: "cc-1-first",
		})
		if state != plan.Blocked {
			t.Errorf("authorised = %v: state = %v, want blocked", authorised, state)
		}
		if !strings.Contains(string(reason), "cc-1-first") {
			t.Errorf("reason %q does not name the conflicted base", reason)
		}
	}
}

// TestStatusPrefersARunToAConflictedBase: a row that has already launched is described by its
// run, not by the state of the base it was cut from.
func TestStatusPrefersARunToAConflictedBase(t *testing.T) {
	t.Parallel()

	state, _ := plan.Status(plan.Facts{
		Unlock:         plan.Unlock{Unlocked: true, BaseBranch: "cc-1-first"},
		Authorised:     true,
		ConflictedBase: "cc-1-first",
		LatestRun:      &plan.RunFact{Alive: true},
	})
	if state != plan.Running {
		t.Errorf("state = %v, want running", state)
	}
}
