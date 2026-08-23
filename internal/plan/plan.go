// Package plan holds the Command Centre's decisions: pure functions over value types.
//
// It imports nothing outside the standard library — not even internal/gh, which execs — so
// that "would this launch, and why is it waiting?" is a table test. api_test.go enforces it.
package plan

import (
	"fmt"
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

// Unlock is the answer to "could this task be cut, and off what?".
type Unlock struct {
	Unlocked   bool
	BaseBranch string
	Reason     Reason
}

// defaultBranch is the base every Phase 1 row is cut from: stacking is off, so an open
// blocker does not become a base (inv. 16).
const defaultBranch = "main"

// Unlocked decides whether a task's blockers are satisfied, over stacking edges only.
//
// Phase 1 keeps stacking off, so every unlocked row bases on main. Phase 2 adds the arms that
// pick a blocker's branch as the base.
func Unlocked(t Task, byURL map[string]Task, prs map[string]PRState) Unlock {
	for _, blockerURL := range t.BlockedBy {
		blocker, ok := byURL[blockerURL]
		if !ok {
			return Unlock{Reason: Reason(fmt.Sprintf("blocked by %s, which is not a tracked task", blockerURL))}
		}
		switch prs[blocker.Branch] {
		case Open, Merged:
			continue
		case Absent, Closed:
			return Unlock{Reason: Reason(fmt.Sprintf(
				"blocked by %s: %s has no open or merged pull request", blockerURL, blocker.Branch))}
		}
	}

	if len(t.BlockedBy) == 0 {
		return Unlock{Unlocked: true, BaseBranch: defaultBranch, Reason: "no blockers"}
	}
	return Unlock{Unlocked: true, BaseBranch: defaultBranch, Reason: "every blocker has a pull request"}
}

// State is a task's derived label. It is never stored: facts are stored, labels are derived
// every tick (inv. 14).
type State int

const (
	Blocked State = iota
	Ready
)

func (s State) String() string {
	switch s {
	case Ready:
		return "ready"
	case Blocked:
		return "blocked"
	default:
		return "blocked"
	}
}

// Facts is everything Status derives from. Now is passed in because this package never calls
// time.Now: the clock is the shell's, which is what makes the rendered page byte-stable.
type Facts struct {
	Task   Task
	Unlock Unlock
	Now    time.Time
}

// Status derives a task's state and the sentence explaining it.
func Status(f Facts) (State, Reason) {
	if !f.Unlock.Unlocked {
		return Blocked, f.Unlock.Reason
	}
	return Ready, f.Unlock.Reason
}
