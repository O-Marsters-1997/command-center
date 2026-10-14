// Package plan holds the Command Centre's decisions: pure functions over value types.
//
// It imports only the standard library and the pure internal/verdict — never internal/gh, which
// execs — so that "would this launch, and why is it waiting?" is a table test. api_test.go enforces it.
package plan

import (
	"fmt"
	"strings"

	"github.com/O-Marsters-1997/command-center/internal/verdict"
)

// PRState is a pull request's state as this package needs it. The zero value is Absent.
// internal/gh owns the wire shape; the shell maps one onto the other.
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
	URL                 string
	Repo                string
	Branch              string
	BlockedBy           []string
	WorkedExampleBranch string
}

type Reason string

// Unlock is the answer to "could this ticket be cut, and off what?". BlockerClosed is true only
// when the blocker's PR closed without merging, not merely absent.
type Unlock struct {
	Unlocked      bool
	BaseBranch    string
	Reason        Reason
	Blocking      []string
	BlockerClosed bool
}

// DefaultBaseBranch is the branch an unstacked ticket cuts from and merges into.
const DefaultBaseBranch = "main"

// Unlocked decides whether a ticket's blockers are satisfied, over stacking edges only: a
// cross-repo blocker feeds the draft gate, never unlock or the base.
func Unlocked(t Ticket, byURL map[string]Ticket, prs map[string]PRState, stacking bool) Unlock {
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
			Blocking:      []string{blocker.URL},
			BlockerClosed: true,
		}
	case Absent:
	}
	return Unlock{
		Reason: Reason(fmt.Sprintf(
			"blocked by %s: %s has no open or merged pull request", blocker.URL, blocker.Branch)),
		Blocking: []string{blocker.URL},
	}
}

func unlockedOnBlockers(blockers []Ticket, prs map[string]PRState) Unlock {
	var unresolved []string
	for _, blocker := range blockers {
		if prs[blocker.Branch] != Merged {
			unresolved = append(unresolved, blocker.URL)
		}
	}
	if len(unresolved) == 0 {
		return Unlock{Unlocked: true, BaseBranch: DefaultBaseBranch, Reason: "every blocker has merged"}
	}
	return Unlock{
		Reason:   Reason(fmt.Sprintf("blocked by %s, not yet merged", strings.Join(unresolved, ", "))),
		Blocking: unresolved,
	}
}

// State is a ticket's derived label. It is never stored: labels are derived every tick.
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

// RunFact is the latest run's liveness and disposition as the loop observed it this tick.
// HasOutcome distinguishes "not yet disposed" from a genuine zero-value Outcome.
type RunFact struct {
	Alive                    bool
	Outcome                  Outcome
	HasOutcome               bool
	LogPath                  string
	PushRefused              bool
	PushRefusedPath          string
	PushFailed               bool
	PROpen                   bool
	PRMerged                 bool
	PRClosedUnmerged         bool
	Verdict                  *verdict.Result
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

// Facts is everything Status derives from. LatestRun is nil until a ticket's first launch.
type Facts struct {
	Unlock          Unlock
	Authorised      bool
	LatestRun       *RunFact
	CancelledMember bool
	ConflictedBase  string
}

// Status derives a ticket's state and the sentence explaining it. A run's liveness and
// disposition outrank the unlocked and authorised facts that mattered only before its first
// launch.
func Status(f Facts) (State, Reason) {
	if f.LatestRun != nil && f.Unlock.BlockerClosed {
		return BaseGone, f.Unlock.Reason
	}
	if f.LatestRun != nil && f.LatestRun.PRMerged {
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

func statusFromRun(run *RunFact, unlock Unlock) (State, Reason, bool) {
	if run == nil {
		return 0, "", false
	}
	if run.Alive {
		return Running, "agent running", true
	}
	if !run.HasOutcome {
		return 0, "", false
	}
	switch run.Outcome {
	case OutcomePush:
		state, reason := statusFromPush(*run, unlock)
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
	case run.Verdict != nil && run.Verdict.Kind != verdict.Checking:
		return stateOfVerdict(*run.Verdict), Reason(run.Verdict.Reason)
	case run.PROpen:
		if run.Verdict != nil {
			return Checking, Reason(run.Verdict.Reason)
		}
		return Checking, "pull request open, no verdict yet"
	case !unlock.Unlocked && len(unlock.Blocking) > 0:
		return PushPending, Reason(fmt.Sprintf("push held: waiting on %s", strings.Join(unlock.Blocking, ", ")))
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

func waitingOnBlockers(blocking []string) Reason {
	if len(blocking) == 1 {
		return Reason(fmt.Sprintf("waiting on %s's PR", blocking[0]))
	}
	return Reason(fmt.Sprintf("waiting on %d blockers' PRs: %s", len(blocking), strings.Join(blocking, ", ")))
}
