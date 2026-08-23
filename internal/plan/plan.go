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

// Task is one tracked ticket.
type Task struct {
	TicketURL string
	Repo      string
	Branch    string
	BlockedBy []string
}

// Reason is the human-readable sentence the page renders on a row.
type Reason string

// Unlock is the answer to "could this task be cut, and off what?". Blocking names the
// same-repo blockers still standing in the way (empty once Unlocked), so a caller building a
// launch preview knows which of them a candidate slice would need to cover.
type Unlock struct {
	Unlocked   bool
	BaseBranch string
	Reason     Reason
	Blocking   []string
}

// defaultBranch is the base every row without a stacked parent is cut from.
const defaultBranch = "main"

// Unlocked decides whether a task's blockers are satisfied, over stacking edges only — a
// cross-repo blocker feeds the Phase-3 draft gate, never unlock or the base (docs/prd-command-
// centre.md § Unlock).
func Unlocked(t Task, byURL map[string]Task, prs map[string]PRState, stacking bool) Unlock {
	var sameRepo []Task
	for _, blockerURL := range t.BlockedBy {
		blocker, ok := byURL[blockerURL]
		if !ok {
			return Unlock{Reason: Reason(fmt.Sprintf("blocked by %s, which is not a tracked task", blockerURL))}
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
func unlockedOnBlocker(blocker Task, prs map[string]PRState, stacking bool) Unlock {
	switch prs[blocker.Branch] {
	case Open:
		base := defaultBranch
		if stacking {
			base = blocker.Branch
		}
		return Unlock{Unlocked: true, BaseBranch: base, Reason: "every blocker has a pull request"}
	case Merged:
		return Unlock{Unlocked: true, BaseBranch: defaultBranch, Reason: "every blocker has a pull request"}
	default: // Absent, Closed
		return Unlock{
			Reason: Reason(fmt.Sprintf(
				"blocked by %s: %s has no open or merged pull request", blocker.TicketURL, blocker.Branch)),
			Blocking: []string{blocker.TicketURL},
		}
	}
}

// unlockedOnBlockers handles fan-in: nothing can be cut from two branches at once, so two or
// more blockers unlock only once every one has merged.
func unlockedOnBlockers(blockers []Task, prs map[string]PRState) Unlock {
	var unresolved []string
	for _, blocker := range blockers {
		if prs[blocker.Branch] != Merged {
			unresolved = append(unresolved, blocker.TicketURL)
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

// State is a task's derived label. It is never stored: facts are stored, labels are derived
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
	case Blocked:
		return "blocked"
	default:
		return "blocked"
	}
}

// RunFact is the latest run's liveness and disposition, as the loop observed it this tick.
// Alive is decided by pid+start-time identity (docs/prd-command-centre.md § A run); HasOutcome
// distinguishes "not yet disposed" from a genuine zero-value Outcome.
type RunFact struct {
	Alive      bool
	Outcome    Outcome
	HasOutcome bool
	LogPath    string
}

// Facts is everything Status derives from. Now is passed in because this package never calls
// time.Now: the clock is the shell's, which is what makes the rendered page byte-stable.
// Authorised is supplied by the caller (membership in an active launch), not derived here.
// LatestRun is nil until a task's first launch — the pre-Phase-3 unlocked × authorised 2x2 is
// exactly what a nil LatestRun still derives.
type Facts struct {
	Task       Task
	Unlock     Unlock
	Now        time.Time
	Authorised bool
	LatestRun  *RunFact
}

// Status derives a task's state and the sentence explaining it. A task with a run derives from
// that run's liveness and disposition first — a live or disposed run outranks the unlocked ×
// authorised facts that only ever mattered before its first launch. Everything else falls back
// to the pre-Phase-3 2x2 (docs/prd-command-centre.md § The states): a queued row must say
// whether it is waiting on a base (hours-or-forever) or a slot (seconds), the two must not
// render alike.
func Status(f Facts) (State, Reason) {
	if state, reason, ok := statusFromRun(f.LatestRun); ok {
		return state, reason
	}

	switch {
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
		return PushPending, "agent finished with commits, waiting to push", true
	case OutcomeCutFailed:
		return CutFailed, "tp new failed to cut a worktree", true
	case OutcomeFailed:
		fallthrough
	default:
		return Failed, Reason(fmt.Sprintf("no commits after this run's baseline; log at %s", run.LogPath)), true
	}
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
