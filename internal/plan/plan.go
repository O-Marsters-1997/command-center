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
	CutFailed
	PushPending
	Checking
	NeedsYou
	PushFailed
	ReviewMe
	PRMerged
	PRClosedUnmerged
	BaseGone
	Cancelled
	BaseMoved
	CIFailed
	RefreshConflicted
	ConflictsWithMain
	VerificationFailed
	WaitingOnProducerDeploy
	AwaitingInput
)

type RunFact struct {
	Alive                    bool
	Outcome                  option.Option[Outcome]
	LogPath                  string
	PushRefused              bool
	PushRefusedPath          string
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
	VerificationFailed       bool
	VerificationFailedReason Reason
	Question                 string
}

type Facts struct {
	Unlock          Unlock
	Authorised      bool
	LatestRun       option.Option[RunFact]
	CancelledMember bool
	ConflictedBase  string
}

func Status(f Facts) (State, Reason) {
	if f.LatestRun.IsSome() && f.Unlock.BlockerClosed {
		return BaseGone, f.Unlock.Reason
	}
	if f.LatestRun.IsSome() && f.LatestRun.Unwrap().PRMerged {
		return PRMerged, "pull request merged"
	}
	if state, reason, ok := statusFromRun(f.LatestRun, f.Unlock); ok {
		return state, reason
	}
	switch {
	case f.CancelledMember:
		return Cancelled, "authorised then cancelled before launching"
	case f.ConflictedBase != "":
		return Blocked, conflictedBaseReason(f.ConflictedBase)
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

func statusFromRun(latest option.Option[RunFact], unlock Unlock) (State, Reason, bool) {
	if latest.IsNone() {
		return 0, "", false
	}
	run := latest.Unwrap()
	if run.Alive {
		return Running, "agent running", true
	}
	if run.Outcome.IsNone() {
		return 0, "", false
	}
	switch run.Outcome.Unwrap() {
	case OutcomePush:
		state, reason := statusFromPush(run, unlock)
		return state, reason, true
	case OutcomeAsked:
		return AwaitingInput, firstLine(run.Question), true
	case OutcomeContinued:
		return Queued, "resuming with your message", true
	case OutcomeCutFailed:
		return CutFailed, "tp new failed to cut a worktree", true
	case OutcomeFailed:
		fallthrough
	default:
		return Failed, Reason(fmt.Sprintf("no commits after this run's baseline; log at %s", run.LogPath)), true
	}
}

func firstLine(s string) Reason {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return Reason(strings.TrimSpace(line))
}

func statusFromPush(run RunFact, unlock Unlock) (State, Reason) {
	switch {
	case run.PRClosedUnmerged:
		return PRClosedUnmerged, "pull request closed without merging"
	case run.MidMerge:
		return RefreshConflicted, "refresh's merge conflicted: the worktree is left mid-merge, resolve it there or abort"
	case run.ConflictsWithMain:
		return ConflictsWithMain, run.ConflictsWithMainReason
	case run.ConflictingPeer != "":
		return Blocked, conflictingPeerReason(run.ConflictingPeer)
	case run.VerificationFailed:
		return VerificationFailed, run.VerificationFailedReason
	case run.PushRefused:
		return NeedsYou, Reason(fmt.Sprintf("push refused: %s touches a protected path", run.PushRefusedPath))
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
	default:
		return PushPending, "agent finished with commits, waiting to push"
	}
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
