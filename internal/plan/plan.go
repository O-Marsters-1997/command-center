package plan

import (
	"fmt"
	"strings"

	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	"github.com/O-Marsters-1997/command-center/internal/plan/verdict"
)

// PRState is a pull request's state as this package needs it. The zero value is Absent.
type PRState int

const (
	Absent PRState = iota
	Open
	Merged
	Closed
)

func (s PRState) String() string {
	switch s {
	case Open:
		return "open"
	case Merged:
		return "merged"
	case Closed:
		return "closed"
	case Absent:
	}
	return "absent"
}

type Ticket struct {
	URL                 TicketURL
	Repo                string
	Branch              string
	BlockedBy           []TicketURL
	WorkedExampleBranch string
	LaunchContext       string
	Choice              AgentChoice
}

func (t Ticket) Ref() BranchRef { return BranchRef{Repo: t.Repo, Branch: t.Branch} }

type Reason string

type Unlock struct {
	Unlocked      bool
	BaseBranch    string
	Reason        Reason
	Blocking      []TicketURL
	BlockerClosed bool
}

const DefaultBaseBranch = "main"

// Unlocked decides whether a ticket's stacking blockers are satisfied.
func Unlocked(t Ticket, byURL map[TicketURL]Ticket, prs map[string]PRState, stacking bool) Unlock {
	var sameRepo []Ticket
	for _, blockerURL := range t.BlockedBy {
		blocker, ok := byURL[blockerURL]
		if !ok {
			return Unlock{Reason: Reason(fmt.Sprintf("blocked by %s, which is not a tracked ticket", blockerURL))}
		}
		if blocker.Repo == t.Repo {
			sameRepo = append(sameRepo, blocker)
		}
	}

	switch len(sameRepo) {
	case 0:
		return Unlock{Unlocked: true, BaseBranch: DefaultBaseBranch, Reason: "no blockers"}
	case 1:
		return unlockedOnBlocker(sameRepo[0], prs, stacking)
	default:
		return unlockedOnBlockers(sameRepo, prs)
	}
}

func unlockedOnBlocker(blocker Ticket, prs map[string]PRState, stacking bool) Unlock {
	switch prs[blocker.Branch] {
	case Open:
		base := DefaultBaseBranch
		if stacking {
			base = blocker.Branch
		}
		return Unlock{Unlocked: true, BaseBranch: base, Reason: "every blocker has a pull request"}
	case Merged:
		return Unlock{Unlocked: true, BaseBranch: DefaultBaseBranch, Reason: "every blocker has a pull request"}
	case Closed:
		return Unlock{
			Reason: Reason(fmt.Sprintf(
				"blocked by %s: %s's pull request was closed without merging", blocker.URL, blocker.Branch)),
			Blocking:      []TicketURL{blocker.URL},
			BlockerClosed: true,
		}
	case Absent:
	}
	return Unlock{
		Reason: Reason(fmt.Sprintf(
			"blocked by %s: %s has no open or merged pull request", blocker.URL, blocker.Branch)),
		Blocking: []TicketURL{blocker.URL},
	}
}

func unlockedOnBlockers(blockers []Ticket, prs map[string]PRState) Unlock {
	var unresolved []TicketURL
	for _, blocker := range blockers {
		if prs[blocker.Branch] != Merged {
			unresolved = append(unresolved, blocker.URL)
		}
	}
	if len(unresolved) == 0 {
		return Unlock{Unlocked: true, BaseBranch: DefaultBaseBranch, Reason: "every blocker has merged"}
	}
	return Unlock{
		Reason:   Reason(fmt.Sprintf("blocked by %s, not yet merged", joinURLs(unresolved))),
		Blocking: unresolved,
	}
}

type State int

const (
	StateUnknown State = iota
	Blocked
	Ready
	Queued
	Running
	Failed
	PushPending
	AwaitingPR
	Checking
	NeedsYou
	PushFailed
	ReviewMe
	PRMerged
	Withdrawn
	Cancelled
	BaseMoved
	CIFailed
	RefreshConflicted
	ConflictsWithMain
	VerificationFailed
	WaitingOnProducerDeploy
	AwaitingInput
	PushRefused
	Parked
	Verifying
	WaitingToVerify
)

type RunFact struct {
	Alive                    bool
	Outcome                  option.Option[Outcome]
	LogPath                  string
	PushRefused              bool
	PushDenied               []PathMatch
	PushFailed               bool
	PROpen                   bool
	PRMerged                 bool
	PRClosedUnmerged         bool
	Verdict                  option.Option[verdict.Result]
	RefreshRefused           bool
	RefreshRefusedReason     Reason
	MidMerge                 bool
	ConflictsWithMain        bool
	ConflictsWithMainReason  Reason
	ConflictingPeer          string
	Verify                   VerifyPhase
	VerificationFailedReason Reason
	Question                 string
	Next                     option.Option[Next]
}

type Facts struct {
	Unlock          Unlock
	Authorised      bool
	LatestRun       option.Option[RunFact]
	CancelledMember bool
	ConflictedBase  string
	Raise           RaiseMode
	RedBlocker      TicketURL
}

func Status(f Facts) (State, Reason) {
	if f.LatestRun.IsSome() && f.Unlock.BlockerClosed {
		return Withdrawn, f.Unlock.Reason
	}
	if f.LatestRun.IsSome() && f.LatestRun.Unwrap().PRMerged {
		return PRMerged, "pull request merged"
	}
	if state, reason, ok := statusFromRun(f.LatestRun, f.Unlock, f.Raise); ok {
		return state, reason
	}
	switch {
	case f.CancelledMember:
		return Cancelled, "authorised then cancelled before launching"
	case f.ConflictedBase != "":
		return Blocked, conflictedBaseReason(f.ConflictedBase)
	case f.Unlock.Unlocked && f.Authorised && f.RedBlocker != "":
		return Queued, Reason(fmt.Sprintf("waiting on %s's CI", f.RedBlocker))
	case f.Unlock.Unlocked && f.Authorised:
		return Queued, "waiting for a slot"
	case f.Unlock.Unlocked:
		return Ready, f.Unlock.Reason
	case f.Authorised:
		return Queued, waitingOnBlockers(f.Unlock.Blocking)
	default:
		return Blocked, f.Unlock.Reason
	}
}

func statusFromRun(latest option.Option[RunFact], unlock Unlock, raise RaiseMode) (State, Reason, bool) {
	run, ok := latest.Get()
	if !ok {
		return StateUnknown, "", false
	}
	if run.Alive {
		return Running, "agent running", true
	}
	outcome, ok := run.Outcome.Get()
	if !ok {
		return StateUnknown, "", false
	}
	switch outcome {
	case OutcomePush:
		state, reason := statusFromPush(run, unlock, raise)
		return state, reason, true
	case OutcomeAsked:
		return AwaitingInput, askedReason(run.Question), true
	case OutcomeContinued:
		return Queued, "resuming with your message", true
	case OutcomePassed:
		if state, reason, ok := statusFromNext(run.Next); ok {
			return state, reason, true
		}
		return Queued, "next step pending", true
	case OutcomeCutFailed:
		return Failed, "cutting the worktree failed", true
	case OutcomeFailed:
		if state, reason, ok := statusFromNext(run.Next); ok {
			return state, reason, true
		}
		fallthrough
	case OutcomeUnknown:
		fallthrough
	default:
		return Failed, Reason(fmt.Sprintf("no commits after this run's baseline; log at %s", run.LogPath)), true
	}
}

func statusFromNext(next option.Option[Next]) (State, Reason, bool) {
	n, ok := next.Get()
	if !ok {
		return StateUnknown, "", false
	}
	switch n.Kind {
	case NextPark:
		return Failed, n.Reason, true
	case NextRun:
		step, ok := n.Step.Get()
		if !ok {
			break
		}
		reason := fmt.Sprintf("next step: %s", step.Name)
		if n.Round > 0 {
			reason += fmt.Sprintf(" (fix round %d)", n.Round)
		}
		return Queued, Reason(reason), true
	case NextNone, NextDone:
	}
	return StateUnknown, "", false
}

const MissingDependencyMarker = "MISSING-DEPENDENCY:"

func askedReason(question string) Reason {
	if rest, ok := strings.CutPrefix(question, MissingDependencyMarker); ok {
		return "blocker work missing from the branch: " + firstLine(strings.TrimSpace(rest))
	}
	return firstLine(question)
}

func firstLine(s string) Reason {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return Reason(strings.TrimSpace(line))
}

func statusFromPush(run RunFact, unlock Unlock, raise RaiseMode) (State, Reason) {
	switch {
	case run.PRClosedUnmerged:
		return Withdrawn, "this ticket's pull request closed without merging"
	case run.MidMerge:
		return RefreshConflicted, "refresh's merge conflicted: the worktree is left mid-merge, resolve it there or abort"
	case run.ConflictsWithMain:
		return ConflictsWithMain, run.ConflictsWithMainReason
	case run.ConflictingPeer != "":
		return PushPending, conflictingPeerReason(run.ConflictingPeer)
	case run.Verify == VerifyRunning:
		return Verifying, "verify command running"
	case run.Verify == VerifyWaiting:
		return WaitingToVerify, "verify command waiting for a slot"
	case run.Verify == VerifyFailed:
		return VerificationFailed, run.VerificationFailedReason
	case run.PushRefused:
		return PushRefused, refusedReason(run.PushDenied)
	case run.PushFailed:
		return PushFailed, "push or pull request creation failed"
	case run.RefreshRefused:
		return NeedsYou, run.RefreshRefusedReason
	case run.Verdict.IsSome() && run.Verdict.Unwrap().Kind != verdict.Checking:
		return stateOfVerdict(run.Verdict.Unwrap()), Reason(run.Verdict.Unwrap().Reason)
	case run.PROpen:
		if run.Verdict.IsSome() {
			return Checking, Reason(run.Verdict.Unwrap().Reason)
		}
		return Checking, "pull request open, no verdict yet"
	case !unlock.Unlocked && len(unlock.Blocking) > 0:
		return PushPending, Reason(fmt.Sprintf("push held: waiting on %s", joinURLs(unlock.Blocking)))
	case !unlock.Unlocked:
		return PushPending, Reason("push held: " + string(unlock.Reason))
	case raise == RaiseManual:
		return AwaitingPR, "agent finished with commits, waiting for you to raise the PR"
	default:
		return PushPending, "agent finished with commits, waiting to push"
	}
}

func refusedReason(denied []PathMatch) Reason {
	parts := make([]string, len(denied))
	for i, m := range denied {
		parts[i] = m.Path
		if m.Rule != "" {
			parts[i] += " (" + m.Rule + ")"
		}
	}
	return Reason("push refused: " + strings.Join(parts, ", "))
}

func stateOfVerdict(r verdict.Result) State {
	switch r.Kind {
	case verdict.BaseMoved:
		return BaseMoved
	case verdict.WaitingOnProducerDeploy:
		return WaitingOnProducerDeploy
	case verdict.ReviewMe:
		return ReviewMe
	case verdict.NeedsYou:
		return NeedsYou
	case verdict.CIFailed:
		return CIFailed
	case verdict.Checking, verdict.KindUnknown:
		return Checking
	}
	return Checking
}

func conflictedBaseReason(base string) Reason {
	return Reason(fmt.Sprintf(
		"%s already carries an unresolved merge conflict: a branch cut from it inherits the conflict", base))
}

func conflictingPeerReason(peer string) Reason {
	return Reason(fmt.Sprintf(
		"%s is a lower-ref open peer this branch conflicts with: only one of a conflicting pair proceeds at a time", peer))
}

func waitingOnBlockers(blocking []TicketURL) Reason {
	if len(blocking) == 1 {
		return Reason(fmt.Sprintf("waiting on %s's PR", blocking[0]))
	}
	return Reason(fmt.Sprintf("waiting on %d blockers' PRs: %s", len(blocking), joinURLs(blocking)))
}
