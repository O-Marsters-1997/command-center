package plan_test

import (
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	"github.com/O-Marsters-1997/command-center/internal/plan/verdict"
)

func TestStatus(t *testing.T) {
	t.Parallel()

	t.Run("before any run", statusBeforeAnyRun)
	t.Run("push pending names the blocker holding the push", statusPushPendingNamesTheBlocker)
	t.Run("cancelled for a member with no run", statusCancelledWithNoRun)
	t.Run("never cancelled once it has run", statusNeverCancelledOnceRun)
	t.Run("with a latest run", statusWithLatestRun)
	t.Run("merged outranks a later run's own disposition", statusMergedOverALaterRun)
	t.Run("failed without a merged PR", statusFailedWithoutAMergedPR)
	t.Run("an undisposed run is not failed", statusUndisposedRunNotFailed)
	t.Run("no latest run", statusNoLatestRun)
	t.Run("base gone outranks anything once it has run", statusBaseGoneOnceRun)
	t.Run("never base gone without a prior run", statusNeverBaseGoneWithoutARun)
	t.Run("blocks on a conflicted base", statusBlocksOnAConflictedBase)
	t.Run("a run outranks a conflicted base", statusRunOverAConflictedBase)
}

func statusBeforeAnyRun(t *testing.T) {
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
			unlock:     plan.Unlock{Reason: "blocked by sandbox://CC-1", Blocking: []plan.TicketURL{"sandbox://CC-1"}},
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
				t.Errorf("Status(facts) state = %v, want %v", state, tt.want)
			}
			if reason != tt.wantReason {
				t.Errorf("Status(facts) reason = %q, want %q", reason, tt.wantReason)
			}
		})
	}
}

func statusPushPendingNamesTheBlocker(t *testing.T) {
	t.Parallel()

	run := option.Some(plan.RunFact{Alive: false, Outcome: option.Some(plan.OutcomePush)})
	held := plan.Unlock{Reason: "blocked", Blocking: []plan.TicketURL{"#259"}}

	state, reason := plan.Status(plan.Facts{Unlock: held, LatestRun: run})
	if state != plan.PushPending || !strings.Contains(string(reason), "#259") {
		t.Errorf("Status(facts) = %v, %q, want push pending naming #259", state, reason)
	}

	twoHeld := plan.Unlock{Blocking: []plan.TicketURL{"#259", "#260"}}
	state, reason = plan.Status(plan.Facts{Unlock: twoHeld, LatestRun: run})
	if state != plan.PushPending || !strings.Contains(string(reason), "#259, #260") {
		t.Errorf("Status(facts) = %v, %q, want both blockers named", state, reason)
	}

	untracked := plan.Unlock{Reason: "blocked by #9, which is not a tracked ticket"}
	state, reason = plan.Status(plan.Facts{Unlock: untracked, LatestRun: run})
	if state != plan.PushPending || !strings.Contains(string(reason), "not a tracked ticket") {
		t.Errorf("Status(facts) = %v, %q, want the unlock reason", state, reason)
	}

	free := plan.Unlock{Unlocked: true, BaseBranch: "main", Reason: "no blockers"}
	state, reason = plan.Status(plan.Facts{Unlock: free, LatestRun: run})
	if state != plan.PushPending || reason != "agent finished with commits, waiting to push" {
		t.Errorf("Status(facts) = %v, %q, want today's push pending reason", state, reason)
	}
}

func statusCancelledWithNoRun(t *testing.T) {
	t.Parallel()

	state, reason := plan.Status(plan.Facts{
		Unlock:          plan.Unlock{Unlocked: true, BaseBranch: "main", Reason: "no blockers"},
		Authorised:      true,
		CancelledMember: true,
	})
	if state != plan.Cancelled {
		t.Errorf("Status(facts) state = %v, want cancelled", state)
	}
	if reason == "" {
		t.Error(`Status(facts) reason = "", want one`)
	}
}

func statusNeverCancelledOnceRun(t *testing.T) {
	t.Parallel()

	state, _ := plan.Status(plan.Facts{
		Unlock:          plan.Unlock{Unlocked: true, BaseBranch: "main", Reason: "no blockers"},
		LatestRun:       option.Some(plan.RunFact{Alive: true}),
		CancelledMember: true,
	})
	if state != plan.Running {
		t.Errorf("Status(facts) state = %v, want running: a row that ever ran is never cancelled", state)
	}
}

func statusWithLatestRun(t *testing.T) {
	t.Parallel()

	unlocked := plan.Unlock{Unlocked: true, BaseBranch: "main", Reason: "no blockers"}

	tests := []struct {
		name         string
		latestRun    option.Option[plan.RunFact]
		wantState    plan.State
		reasonHas    string
		reasonHasLog string
	}{
		{
			name:      "an alive run derives running regardless of unlock or authorisation",
			latestRun: option.Some(plan.RunFact{Alive: true}),
			wantState: plan.Running,
		},
		{
			name: "a dead run with no commits derives failed, naming the log path",
			latestRun: option.Some(plan.RunFact{
				Alive: false, Outcome: option.Some(plan.OutcomeFailed), LogPath: "/state/runs/7.jsonl",
			}),
			wantState:    plan.Failed,
			reasonHas:    "no commits",
			reasonHasLog: "/state/runs/7.jsonl",
		},
		{
			name: "a dead run with commits derives push pending",
			latestRun: option.Some(plan.RunFact{
				Alive: false, Outcome: option.Some(plan.OutcomePush), LogPath: "/state/runs/8.jsonl",
			}),
			wantState: plan.PushPending,
		},
		{
			name: "an asked run derives awaiting input with the question's first line",
			latestRun: option.Some(plan.RunFact{
				Alive: false, Outcome: option.Some(plan.OutcomeAsked),
				Question: "Which database?\nPostgres or SQLite.",
			}),
			wantState: plan.AwaitingInput,
			reasonHas: "Which database?",
		},
		{
			name: "a continued run derives queued while the resume is pending",
			latestRun: option.Some(plan.RunFact{
				Alive: false, Outcome: option.Some(plan.OutcomeContinued),
			}),
			wantState: plan.Queued,
			reasonHas: "resuming with your message",
		},
		{
			name: "a failed cut derives cut failed",
			latestRun: option.Some(plan.RunFact{
				Alive: false, Outcome: option.Some(plan.OutcomeCutFailed),
			}),
			wantState: plan.CutFailed,
		},
		{
			name: "a refused push derives needs you, naming the path",
			latestRun: option.Some(plan.RunFact{
				Alive: false, Outcome: option.Some(plan.OutcomePush),
				PushRefused: true, PushRefusedPath: ".github/workflows/ci.yml",
			}),
			wantState: plan.NeedsYou,
			reasonHas: ".github/workflows/ci.yml",
		},
		{
			name: "a push or PR-create failure derives push failed",
			latestRun: option.Some(plan.RunFact{
				Alive: false, Outcome: option.Some(plan.OutcomePush), PushFailed: true,
			}),
			wantState: plan.PushFailed,
		},
		{
			name: "an open PR with no verdict yet derives checking",
			latestRun: option.Some(plan.RunFact{
				Alive: false, Outcome: option.Some(plan.OutcomePush), PROpen: true,
			}),
			wantState: plan.Checking,
			reasonHas: "no verdict yet",
		},
		{
			name: "an open PR with a checking verdict derives checking, naming the verdict's reason",
			latestRun: option.Some(plan.RunFact{
				Alive: false, Outcome: option.Some(plan.OutcomePush), PROpen: true,
				Verdict: option.Some(verdict.Result{Kind: verdict.Checking, Reason: "check config changed"}),
			}),
			wantState: plan.Checking,
			reasonHas: "check config changed",
		},
		{
			name: "an open PR with a review-me verdict derives review me",
			latestRun: option.Some(plan.RunFact{
				Alive: false, Outcome: option.Some(plan.OutcomePush), PROpen: true,
				Verdict: option.Some(verdict.Result{Kind: verdict.ReviewMe, Reason: "every required check passed"}),
			}),
			wantState: plan.ReviewMe,
			reasonHas: "every required check passed",
		},
		{
			name: "an open PR with a needs-you verdict derives needs you over push facts alone",
			latestRun: option.Some(plan.RunFact{
				Alive: false, Outcome: option.Some(plan.OutcomePush), PROpen: true,
				Verdict: option.Some(verdict.Result{Kind: verdict.NeedsYou, Reason: "a required check failed"}),
			}),
			wantState: plan.NeedsYou,
			reasonHas: "a required check failed",
		},
		{
			name: "commits with no push attempt yet still derives push pending",
			latestRun: option.Some(plan.RunFact{
				Alive: false, Outcome: option.Some(plan.OutcomePush),
			}),
			wantState: plan.PushPending,
		},
		{
			name: "the pull request having merged outranks an open-PR verdict",
			latestRun: option.Some(plan.RunFact{
				Alive: false, Outcome: option.Some(plan.OutcomePush), PROpen: false, PRMerged: true,
				Verdict: option.Some(verdict.Result{Kind: verdict.ReviewMe, Reason: "every required check passed"}),
			}),
			wantState: plan.PRMerged,
		},
		{
			name: "the pull request having closed unmerged outranks a would-be checking state",
			latestRun: option.Some(plan.RunFact{
				Alive: false, Outcome: option.Some(plan.OutcomePush), PRClosedUnmerged: true,
			}),
			wantState: plan.PRClosedUnmerged,
		},
		{
			name: "a waiting-on-producer-deploy verdict derives waiting on producer deploy",
			latestRun: option.Some(plan.RunFact{
				Alive: false, Outcome: option.Some(plan.OutcomePush), PROpen: true,
				Verdict: option.Some(verdict.Result{
					Kind:   verdict.WaitingOnProducerDeploy,
					Reason: "every required check passed except the compat check",
				}),
			}),
			wantState: plan.WaitingOnProducerDeploy,
			reasonHas: "except the compat check",
		},
		{
			name: "a base-moved verdict derives base moved even over a needs-you check reading",
			latestRun: option.Some(plan.RunFact{
				Alive: false, Outcome: option.Some(plan.OutcomePush), PROpen: true,
				Verdict: option.Some(verdict.Result{
					Kind:   verdict.BaseMoved,
					Reason: "base moved: the parent advanced past what this branch was cut from",
				}),
			}),
			wantState: plan.BaseMoved,
			reasonHas: "the parent advanced",
		},
		{
			name: "a refused fast-forward derives needs you naming the reason, outranking base moved",
			latestRun: option.Some(plan.RunFact{
				Alive: false, Outcome: option.Some(plan.OutcomePush), PROpen: true,
				Verdict: option.Some(verdict.Result{Kind: verdict.BaseMoved}), RefreshRefused: true,
				RefreshRefusedReason: "not a fast-forward: diverged from origin/cc-1",
			}),
			wantState: plan.NeedsYou,
			reasonHas: "not a fast-forward",
		},
		{
			name: "an unresolved merge derives refresh conflicted over base moved and a refused fast-forward",
			latestRun: option.Some(plan.RunFact{
				Alive: false, Outcome: option.Some(plan.OutcomePush), PROpen: true,
				MidMerge: true, Verdict: option.Some(verdict.Result{Kind: verdict.BaseMoved}), RefreshRefused: true,
				RefreshRefusedReason: "not a fast-forward: diverged from origin/cc-1",
			}),
			wantState: plan.RefreshConflicted,
			reasonHas: "mid-merge",
		},
		{
			name: "an unresolved merge outranks a push failure, whose re-run would spawn into it",
			latestRun: option.Some(plan.RunFact{
				Alive: false, Outcome: option.Some(plan.OutcomePush), MidMerge: true, PushFailed: true,
			}),
			wantState: plan.RefreshConflicted,
		},
		{
			name: "a merged pull request outranks an unresolved merge in the worktree",
			latestRun: option.Some(plan.RunFact{
				Alive: false, Outcome: option.Some(plan.OutcomePush), PRMerged: true, MidMerge: true,
			}),
			wantState: plan.PRMerged,
		},
		{
			name: "a branch that no longer merges into main derives conflicts with main, naming the branch",
			latestRun: option.Some(plan.RunFact{
				Alive: false, Outcome: option.Some(plan.OutcomePush), PROpen: true,
				ConflictsWithMain: true, ConflictsWithMainReason: "cc-9-example no longer merges cleanly into main",
			}),
			wantState: plan.ConflictsWithMain,
			reasonHas: "no longer merges cleanly into main",
		},
		{
			name: "conflicts with main outranks a refused fast-forward and a base-moved verdict",
			latestRun: option.Some(plan.RunFact{
				Alive: false, Outcome: option.Some(plan.OutcomePush), PROpen: true,
				ConflictsWithMain: true, ConflictsWithMainReason: "cc-9-example no longer merges cleanly into main",
				Verdict: option.Some(verdict.Result{Kind: verdict.BaseMoved}), RefreshRefused: true,
				RefreshRefusedReason: "not a fast-forward: diverged from origin/cc-1",
			}),
			wantState: plan.ConflictsWithMain,
		},
		{
			name: "an unresolved merge outranks conflicts with main, whose refresh would spawn into it",
			latestRun: option.Some(plan.RunFact{
				Alive: false, Outcome: option.Some(plan.OutcomePush), MidMerge: true, ConflictsWithMain: true,
			}),
			wantState: plan.RefreshConflicted,
		},
		{
			name: "a merged pull request outranks a branch that no longer merges into main",
			latestRun: option.Some(plan.RunFact{
				Alive: false, Outcome: option.Some(plan.OutcomePush), PRMerged: true, ConflictsWithMain: true,
			}),
			wantState: plan.PRMerged,
		},
		{
			name: "a conflicting peer derives blocked, naming the peer",
			latestRun: option.Some(plan.RunFact{
				Alive: false, Outcome: option.Some(plan.OutcomePush), PROpen: true,
				ConflictingPeer: "cc-9-lower-ref",
			}),
			wantState: plan.Blocked,
			reasonHas: "cc-9-lower-ref",
		},
		{
			name: "conflicts with main outranks a conflicting peer",
			latestRun: option.Some(plan.RunFact{
				Alive: false, Outcome: option.Some(plan.OutcomePush), PROpen: true,
				ConflictsWithMain: true, ConflictsWithMainReason: "cc-9-example no longer merges cleanly into main",
				ConflictingPeer: "cc-9-lower-ref",
			}),
			wantState: plan.ConflictsWithMain,
		},
		{
			name: "a conflicting peer outranks a review-me verdict, which it suppresses",
			latestRun: option.Some(plan.RunFact{
				Alive: false, Outcome: option.Some(plan.OutcomePush), PROpen: true,
				ConflictingPeer: "cc-9-lower-ref",
				Verdict:         option.Some(verdict.Result{Kind: verdict.ReviewMe, Reason: "every required check passed"}),
			}),
			wantState: plan.Blocked,
			reasonHas: "cc-9-lower-ref",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			state, reason := plan.Status(plan.Facts{Unlock: unlocked, Authorised: false, LatestRun: tt.latestRun})
			if state != tt.wantState {
				t.Errorf("Status(facts) state = %v, want %v (reason %q)", state, tt.wantState, reason)
			}
			if tt.reasonHas != "" && !strings.Contains(string(reason), tt.reasonHas) {
				t.Errorf("Status(facts) reason = %q, want it to contain %q", reason, tt.reasonHas)
			}
			if tt.reasonHasLog != "" && !strings.Contains(string(reason), tt.reasonHasLog) {
				t.Errorf("Status(facts) reason = %q, want it to contain %q", reason, tt.reasonHasLog)
			}
		})
	}
}

func statusMergedOverALaterRun(t *testing.T) {
	t.Parallel()

	unlocked := plan.Unlock{Unlocked: true, BaseBranch: "main", Reason: "no blockers"}

	tests := []struct {
		name      string
		latestRun option.Option[plan.RunFact]
	}{
		{
			name: "a later failed run with no push row (#251)",
			latestRun: option.Some(plan.RunFact{
				Alive: false, Outcome: option.Some(plan.OutcomeFailed), PRMerged: true,
			}),
		},
		{
			name: "a later failed cut (#251)",
			latestRun: option.Some(plan.RunFact{
				Alive: false, Outcome: option.Some(plan.OutcomeCutFailed), PRMerged: true,
			}),
		},
		{
			name: "a failed run whose branch was pushed and merged outside the daemon (#234)",
			latestRun: option.Some(plan.RunFact{
				Alive: false, Outcome: option.Some(plan.OutcomeFailed), LogPath: "/state/runs/4.jsonl",
				PRMerged: true,
			}),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			state, reason := plan.Status(plan.Facts{Unlock: unlocked, Authorised: false, LatestRun: tt.latestRun})
			if state != plan.PRMerged {
				t.Errorf("Status(facts) state = %v, want merged", state)
			}
			if reason != "pull request merged" {
				t.Errorf("Status(facts) reason = %q, want %q", reason, "pull request merged")
			}
		})
	}
}

func statusFailedWithoutAMergedPR(t *testing.T) {
	t.Parallel()

	unlocked := plan.Unlock{Unlocked: true, BaseBranch: "main", Reason: "no blockers"}
	state, _ := plan.Status(plan.Facts{
		Unlock: unlocked, Authorised: false,
		LatestRun: option.Some(plan.RunFact{Alive: false, Outcome: option.Some(plan.OutcomeFailed)}),
	})
	if state != plan.Failed {
		t.Errorf("Status(facts) state = %v, want failed", state)
	}
}

func statusUndisposedRunNotFailed(t *testing.T) {
	t.Parallel()

	state, _ := plan.Status(plan.Facts{
		Unlock:    plan.Unlock{Unlocked: true, BaseBranch: "main", Reason: "no blockers"},
		LatestRun: option.Some(plan.RunFact{Alive: false, Outcome: option.None[plan.Outcome]()}),
	})
	if state == plan.Failed {
		t.Errorf("Status(facts) state = %v, want anything but failed for a run with no outcome", state)
	}
}

func statusNoLatestRun(t *testing.T) {
	t.Parallel()

	state, reason := plan.Status(plan.Facts{
		Unlock:     plan.Unlock{Unlocked: true, BaseBranch: "main", Reason: "no blockers"},
		Authorised: false,
	})
	if state != plan.Ready || reason != "no blockers" {
		t.Errorf("Status(facts) = %v, %q, want ready, \"no blockers\"", state, reason)
	}
}

func statusBaseGoneOnceRun(t *testing.T) {
	t.Parallel()

	blockerClosed := plan.Unlock{
		Reason:        "blocked by sandbox://CC-1: cc-1's pull request was closed without merging",
		BlockerClosed: true,
	}

	tests := []struct {
		name      string
		latestRun option.Option[plan.RunFact]
	}{
		{name: "a live run", latestRun: option.Some(plan.RunFact{Alive: true})},
		{name: "a failed run", latestRun: option.Some(plan.RunFact{Outcome: option.Some(plan.OutcomeFailed)})},
		{name: "a run still waiting to push", latestRun: option.Some(plan.RunFact{Outcome: option.Some(plan.OutcomePush)})},
		{
			name:      "a run already checking",
			latestRun: option.Some(plan.RunFact{Outcome: option.Some(plan.OutcomePush), PROpen: true}),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			state, reason := plan.Status(plan.Facts{Unlock: blockerClosed, Authorised: true, LatestRun: tt.latestRun})
			if state != plan.BaseGone {
				t.Errorf("Status(facts) state = %v, want base_gone", state)
			}
			if reason != blockerClosed.Reason {
				t.Errorf("Status(facts) reason = %q, want the unlock reason %q", reason, blockerClosed.Reason)
			}
		})
	}
}

func statusNeverBaseGoneWithoutARun(t *testing.T) {
	t.Parallel()

	blockerClosed := plan.Unlock{
		Reason: "blocked by sandbox://CC-1", BlockerClosed: true, Blocking: []plan.TicketURL{"sandbox://CC-1"},
	}

	state, _ := plan.Status(plan.Facts{Unlock: blockerClosed, Authorised: false, LatestRun: option.None[plan.RunFact]()})
	if state != plan.Blocked {
		t.Errorf("Status(facts) state = %v, want blocked: no run ever happened", state)
	}

	state, _ = plan.Status(plan.Facts{Unlock: blockerClosed, Authorised: true, LatestRun: option.None[plan.RunFact]()})
	if state != plan.Queued {
		t.Errorf("Status(facts) state = %v, want queued: authorised but never launched", state)
	}
}

func statusBlocksOnAConflictedBase(t *testing.T) {
	t.Parallel()

	for _, authorised := range []bool{false, true} {
		state, reason := plan.Status(plan.Facts{
			Unlock:         plan.Unlock{Unlocked: true, BaseBranch: "cc-1-first", Reason: "every blocker has a pull request"},
			Authorised:     authorised,
			ConflictedBase: "cc-1-first",
		})
		if state != plan.Blocked {
			t.Errorf("Status(authorised %t) state = %v, want blocked", authorised, state)
		}
		if !strings.Contains(string(reason), "cc-1-first") {
			t.Errorf("Status(authorised %t) reason = %q, want it to name cc-1-first", authorised, reason)
		}
	}
}

func statusRunOverAConflictedBase(t *testing.T) {
	t.Parallel()

	state, _ := plan.Status(plan.Facts{
		Unlock:         plan.Unlock{Unlocked: true, BaseBranch: "cc-1-first"},
		Authorised:     true,
		ConflictedBase: "cc-1-first",
		LatestRun:      option.Some(plan.RunFact{Alive: true}),
	})
	if state != plan.Running {
		t.Errorf("Status(facts) state = %v, want running", state)
	}
}
