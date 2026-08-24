package plan

import (
	"fmt"
	"strings"
)

// GatingBlockers returns t's blockers in another repo -- edges Unlocked skips entirely (a
// cross-repo blocker never affects unlock or the base) but DraftGate needs
// (docs/designs/command-centre-design.md § 4, § 6 job 2).
func GatingBlockers(t Task, byURL map[string]Task) []Task {
	var gating []Task
	for _, blockerURL := range t.BlockedBy {
		blocker, ok := byURL[blockerURL]
		if ok && blocker.Repo != t.Repo {
			gating = append(gating, blocker)
		}
	}
	return gating
}

// OpensAsDraft decides whether a task's pull request should be created as a draft: any task
// with a gating edge or a seam (docs/designs/command-centre-design.md § 6 job 2). DraftGate
// decides the steady state; this is the one-off creation-time call.
func OpensAsDraft(t Task, byURL map[string]Task) bool {
	return len(GatingBlockers(t, byURL)) > 0 || len(t.Seams) > 0
}

// DraftGate decides whether a consumer's pull request should stay a draft: any gating blocker
// unmerged, or the consumer's own verdict not green. It never asks to re-draft -- GitHub has no
// such affordance (docs/designs/command-centre-design.md § 6 job 2, inv. 13).
func DraftGate(gating []Task, prs map[string]PRState, verdictGreen bool) (draft bool, reason Reason) {
	var closed, unresolved []string
	for _, g := range gating {
		switch prs[g.Branch] {
		case Merged:
		case Closed:
			closed = append(closed, g.TicketURL)
		default: // Open or Absent
			unresolved = append(unresolved, g.TicketURL)
		}
	}

	if len(closed) > 0 {
		return true, closedGateReason(closed)
	}
	if len(unresolved) > 0 {
		return true, unresolvedGateReason(unresolved)
	}
	if !verdictGreen {
		return true, "waiting on its own checks"
	}
	return false, "every gating blocker has merged and its own checks are green"
}

// unresolvedGateReason names a gating blocker whose pull request has not yet merged -- open or
// entirely absent, neither of which un-drafts the consumer.
func unresolvedGateReason(tickets []string) Reason {
	if len(tickets) == 1 {
		return Reason(fmt.Sprintf("waiting on %s", tickets[0]))
	}
	return Reason(fmt.Sprintf("waiting on %d blockers: %s", len(tickets), strings.Join(tickets, ", ")))
}

// closedGateReason names a gating blocker whose pull request closed without merging: the
// consumer is drafted permanently, and the reason says so rather than reading as a transient wait.
func closedGateReason(tickets []string) Reason {
	if len(tickets) == 1 {
		return Reason(fmt.Sprintf("waiting on %s: its pull request was closed without merging", tickets[0]))
	}
	return Reason(fmt.Sprintf("waiting on %d blockers whose pull requests closed without merging: %s",
		len(tickets), strings.Join(tickets, ", ")))
}
