// Package plan holds the Command Centre's decisions: pure functions over value types.
//
// It imports nothing outside the standard library — not even internal/gh, which execs — so
// that "would this launch, and why is it waiting?" is a table test. api_test.go enforces it.
package plan

import (
	"fmt"
	"strings"
	"time"
)

// PRState is a pull request's state as this package needs it. The zero value is Absent.
// internal/gh owns the wire shape; the shell maps one onto the other, which is what keeps
// this package free of gh's exec dependency.
type PRState int

const (
	Absent PRState = iota
	Open
	Merged
	Closed
)

// Ticket is one tracked ticket.
type Ticket struct {
	URL       string
	Repo      string
	Branch    string
	BlockedBy []string
}

// Reason is the human-readable sentence the page renders on a row.
type Reason string

// Unlock is the answer to "could this ticket be cut, and off what?" for one ticket.
// BlockerClosed is true only when the blocker's PR closed without merging, not merely absent,
// which lets an already-run row derive `base gone` instead of `blocked` (inv. 19).
type Unlock struct {
	Unlocked      bool
	BaseBranch    string
	Reason        Reason
	Blocking      []string
	BlockerClosed bool
}

// defaultBranch is the base every row without a stacked parent is cut from.
const defaultBranch = "main"

// Unlocked decides whether a ticket's blockers are satisfied, over stacking edges only — a
// cross-repo blocker feeds the Phase-3 draft gate, never unlock or the base
// (docs/prds/prd-command-centre.md § Unlock).
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
		return Unlock{Unlocked: true, BaseBranch: defaultBranch, Reason: "no blockers"}
	case 1:
		return unlockedOnBlocker(sameRepo[0], prs, stacking)
	default:
		return unlockedOnBlockers(sameRepo, prs)
	}
}

// unlockedOnBlocker handles the single-blocker arms: an open PR unlocks off the blocker's
// branch when stacking is on, a merged one always unlocks off main (both repos delete
// branches on merge, so the blocker's branch no longer exists to base on).
func unlockedOnBlocker(blocker Ticket, prs map[string]PRState, stacking bool) Unlock {
	switch prs[blocker.Branch] {
	case Open:
		base := defaultBranch
		if stacking {
			base = blocker.Branch
		}
		return Unlock{Unlocked: true, BaseBranch: base, Reason: "every blocker has a pull request"}
	case Merged:
		return Unlock{Unlocked: true, BaseBranch: defaultBranch, Reason: "every blocker has a pull request"}
	case Closed:
		return Unlock{
			Reason: Reason(fmt.Sprintf(
				"blocked by %s: %s's pull request was closed without merging", blocker.URL, blocker.Branch)),
			Blocking:      []string{blocker.URL},
			BlockerClosed: true,
		}
	default: // Absent
		return Unlock{
			Reason: Reason(fmt.Sprintf(
				"blocked by %s: %s has no open or merged pull request", blocker.URL, blocker.Branch)),
			Blocking: []string{blocker.URL},
		}
	}
}

// unlockedOnBlockers handles fan-in: nothing can be cut from two branches at once, so two or
// more blockers unlock only once every one has merged.
func unlockedOnBlockers(blockers []Ticket, prs map[string]PRState) Unlock {
	var unresolved []string
	for _, blocker := range blockers {
		if prs[blocker.Branch] != Merged {
			unresolved = append(unresolved, blocker.URL)
		}
	}
	if len(unresolved) == 0 {
		return Unlock{Unlocked: true, BaseBranch: defaultBranch, Reason: "every blocker has merged"}
	}
	return Unlock{
		Reason:   Reason(fmt.Sprintf("blocked by %s, not yet merged", strings.Join(unresolved, ", "))),
		Blocking: unresolved,
	}
}

// State is a ticket's derived label. It is never stored: facts are stored, labels are derived
// every tick (inv. 14).
type State int

const (
	Blocked State = iota
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
	// PRMerged is prefixed (unlike its siblings) because plan.Merged already names a PRState
	// value (§2's naming-collision precedent, as with Refused in issue #5) — flagged rather
	// than silently working around it.
	PRMerged
	PRClosedUnmerged
	BaseGone
	Cancelled
	// BaseMoved is derived from RunFact.VerdictBaseMoved, never a stored column (inv. 14), and
	// verdict checks its expiry ahead of the predicate so a red descendant whose base moved is
	// not read as needs_you (docs/designs/command-centre-design.md § 4a).
	BaseMoved
	// RefreshConflicted is derived from RunFact.MidMerge, read from the worktree's MERGE_HEAD
	// every tick, so a human who resolves the conflict by hand and commits clears the state with
	// no verb (docs/designs/command-centre-design.md § 4a).
	RefreshConflicted
	// ConflictsWithMain is derived from RunFact.ConflictsWithMain, this branch's own git
	// merge-tree reading against origin/main (docs/adr/0006-resolve-a-conflict-once.md), never
	// GitHub's mergeable field. refresh is the only verb that clears it.
	ConflictsWithMain
	// VerificationFailed is derived from RunFact.VerificationFailed: a clean merge-forward or
	// restack whose repo-configured verify command then failed (issue #110).
	VerificationFailed
	// WaitingOnProducerDeploy is derived from RunFact.VerdictWaitingOnProducer, internal/verdict's
	// own inv. 12 reading -- the cross-repo compat check was the sole red required check
	// (docs/designs/command-centre-design.md § 11 inv. 12).
	WaitingOnProducerDeploy
	stateCount
)

func (s State) String() string {
	switch s {
	case Ready:
		return "ready"
	case Queued:
		return "queued"
	case Running:
		return "running"
	case Failed:
		return "failed"
	case CutFailed:
		return "cut_failed"
	case PushPending:
		return "push_pending"
	case Checking:
		return "checking"
	case NeedsYou:
		return "needs_you"
	case PushFailed:
		return "push_failed"
	case ReviewMe:
		return "review_me"
	case PRMerged:
		return "merged"
	case PRClosedUnmerged:
		return "pr_closed_unmerged"
	case BaseGone:
		return "base_gone"
	case Cancelled:
		return "cancelled"
	case BaseMoved:
		return "base_moved"
	case RefreshConflicted:
		return "refresh_conflicted"
	case ConflictsWithMain:
		return "conflicts_with_main"
	case VerificationFailed:
		return "verification_failed"
	case WaitingOnProducerDeploy:
		return "waiting_on_producer_deploy"
	case Blocked:
		return "blocked"
	default:
		return "blocked"
	}
}

// RunFact is the latest run's liveness and disposition, as the loop observed it this tick.
// Alive is decided by pid+start-time identity (docs/prds/prd-command-centre.md § A run); HasOutcome
// distinguishes "not yet disposed" from a genuine zero-value Outcome.
type RunFact struct {
	Alive      bool
	Outcome    Outcome
	HasOutcome bool
	LogPath    string
	// Push* fields matter only when Outcome == OutcomePush: this tick's own push-policy and
	// push/PR-create result (docs/prds/prd-command-centre.md § Phase 4). PROpen comes from the
	// observation's own PR snapshot for this ticket's branch, not a stored column (inv. 14).
	PushRefused     bool
	PushRefusedPath string
	PushFailed      bool
	PROpen          bool
	// PRMerged and PRClosedUnmerged read this ticket's own branch's PR state, never the blocker's
	// (that is Unlock.BlockerClosed's job). Once GitHub says merged or closed, that outranks
	// every other push fact below (docs/prds/prd-command-centre.md § The states).
	PRMerged         bool
	PRClosedUnmerged bool
	// Verdict* fields matter only once PROpen: internal/cc's call to internal/verdict's pure
	// Evaluate, mapped to booleans since this package cannot import that one (issue #2 AC12).
	// Neither set means "no predicate configured, or still checking" — VerdictReason then carries
	// whatever cc computed, else empty. VerdictBaseMoved is internal/verdict's own expiry (§4a),
	// checked ahead of the predicate, so it can be true however the other two read.
	VerdictReviewMe          bool
	VerdictNeedsYou          bool
	VerdictBaseMoved         bool
	VerdictWaitingOnProducer bool
	VerdictReason            Reason
	// RefreshRefused is set when refresh's own fast-forward step (§4a step 2) last failed: the
	// row reads needs_you naming the reason, and the automatic pass (internal/cc/refresh.go)
	// never retries it -- only the refresh verb does.
	RefreshRefused       bool
	RefreshRefusedReason Reason
	// MidMerge is set while the worktree holds an unresolved merge -- refresh's step 3 conflicted
	// (docs/designs/command-centre-design.md § 4a). It is read from MERGE_HEAD, never stored, and
	// outranks every push and verdict fact: re-run must not spawn an agent into a mid-merge worktree.
	MidMerge bool
	// ConflictsWithMain is set when this ticket's own pushed branch no longer merges cleanly into
	// main, read from git merge-tree, never GitHub's mergeable field
	// (docs/adr/0006-resolve-a-conflict-once.md). It outranks every fact below it, as MidMerge does.
	ConflictsWithMain       bool
	ConflictsWithMainReason Reason
	// VerificationFailed is set when a clean refresh or restack's configured verify command last
	// failed since this ticket's last recorded push (issue #110). It outranks every push and
	// verdict fact below, as MidMerge and ConflictsWithMain do.
	VerificationFailed       bool
	VerificationFailedReason Reason
}

// Facts is everything Status derives from. Now is passed in because this package never calls
// time.Now: the clock is the shell's, which keeps the rendered page byte-stable. LatestRun is
// nil until a ticket's first launch, which the pre-Phase-3 unlocked × authorised 2x2 still derives.
type Facts struct {
	Ticket          Ticket
	Unlock          Unlock
	Now             time.Time
	Authorised      bool
	LatestRun       *RunFact
	CancelledMember bool
	// ConflictedBase names the base a launch would cut this task from when that base already
	// carries a conflict, and is empty when it is clean. A row that has already run is described
	// by its run instead (docs/adr/0006-resolve-a-conflict-once.md).
	ConflictedBase string
}

// Status derives a ticket's state and the sentence explaining it. A run's liveness and disposition
// outrank the unlocked × authorised facts that mattered only before its first launch. A queued
// row must say whether it's waiting on a base (hours) or a slot (seconds)
// (docs/prds/prd-command-centre.md § The states).
func Status(f Facts) (State, Reason) {
	// A row that has ever run never returns to blocked (inv. 19): once its blocker's pull
	// request is closed without merging, the premise it launched under is withdrawn, and that
	// outranks whatever its own run, push or verdict facts would otherwise say — running,
	// checking, needs you, all of it.
	if f.LatestRun != nil && f.Unlock.BlockerClosed {
		return BaseGone, f.Unlock.Reason
	}
	if state, reason, ok := statusFromRun(f.LatestRun); ok {
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

// statusFromRun derives a state from the latest run, when it has anything conclusive to say: a
// live process, or a disposed one. A run that is neither (dead but not yet disposed) reports ok
// = false rather than guessing, since the loop always disposes a run in the same tick it finds
// it dead — that combination should not reach a persisted Facts.
func statusFromRun(run *RunFact) (State, Reason, bool) {
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
		state, reason := statusFromPush(*run)
		return state, reason, true
	case OutcomeCutFailed:
		return CutFailed, "tp new failed to cut a worktree", true
	case OutcomeFailed:
		fallthrough
	default:
		return Failed, Reason(fmt.Sprintf("no commits after this run's baseline; log at %s", run.LogPath)), true
	}
}

// statusFromPush refines a push-outcome run's state from this tick's own push facts: a policy
// hit refuses outright, a push or PR-create failure needs a human's retry, an open PR hands off
// to the verdict step (Phase 5), and otherwise the push is still pending.
func statusFromPush(run RunFact) (State, Reason) {
	switch {
	case run.PRMerged:
		return PRMerged, "pull request merged"
	case run.PRClosedUnmerged:
		return PRClosedUnmerged, "pull request closed without merging"
	case run.MidMerge:
		return RefreshConflicted, "refresh's merge conflicted: the worktree is left mid-merge, resolve it there or abort"
	case run.ConflictsWithMain:
		return ConflictsWithMain, run.ConflictsWithMainReason
	case run.VerificationFailed:
		return VerificationFailed, run.VerificationFailedReason
	case run.PushRefused:
		return NeedsYou, Reason(fmt.Sprintf("push refused: %s touches a protected path", run.PushRefusedPath))
	case run.PushFailed:
		return PushFailed, "push or pull request creation failed"
	case run.RefreshRefused:
		return NeedsYou, run.RefreshRefusedReason
	case run.VerdictBaseMoved:
		return BaseMoved, run.VerdictReason
	case run.VerdictWaitingOnProducer:
		return WaitingOnProducerDeploy, run.VerdictReason
	case run.VerdictReviewMe:
		return ReviewMe, run.VerdictReason
	case run.VerdictNeedsYou:
		return NeedsYou, run.VerdictReason
	case run.PROpen:
		reason := run.VerdictReason
		if reason == "" {
			reason = "pull request open, no verdict yet"
		}
		return Checking, reason
	default:
		return PushPending, "agent finished with commits, waiting to push"
	}
}

// conflictedBaseReason is the sentence a row and a preview both show for a base that already
// carries a conflict, so the two surfaces never explain the same refusal differently.
func conflictedBaseReason(base string) Reason {
	return Reason(fmt.Sprintf(
		"%s already carries an unresolved merge conflict: a branch cut from it inherits the conflict", base))
}

// waitingOnBlockers renders the reason a queued-but-locked row is still waiting: naming a
// single blocker reads better than the unlock reason's generic "blocked by X, not yet merged"
// sentence, and PRD § The page requires four such rows to read as one problem, not four.
func waitingOnBlockers(blocking []string) Reason {
	if len(blocking) == 1 {
		return Reason(fmt.Sprintf("waiting on %s's PR", blocking[0]))
	}
	return Reason(fmt.Sprintf("waiting on %d blockers' PRs: %s", len(blocking), strings.Join(blocking, ", ")))
}
