package plan

import (
	"fmt"
	"strings"
)

// GatingBlockers returns t's blockers in another repo: edges Unlocked skips entirely but
// DraftGate needs.
func GatingBlockers(t Ticket, byURL map[string]Ticket) []Ticket {
	var gating []Ticket
	for _, blockerURL := range t.BlockedBy {
		blocker, ok := byURL[blockerURL]
		if ok && blocker.Repo != t.Repo {
			gating = append(gating, blocker)
		}
	}
	return gating
}

// OpensAsDraft decides whether a ticket's pull request is created as a draft: any ticket with a
// gating edge. DraftGate decides the steady state afterwards.
func OpensAsDraft(t Ticket, byURL map[string]Ticket) bool {
	return len(GatingBlockers(t, byURL)) > 0
}

// DraftGate decides whether a consumer's pull request stays a draft: any gating blocker
// unmerged, or its own verdict not green. It never asks to re-draft, as GitHub has no such
// affordance.
func DraftGate(gating []Ticket, prs map[string]PRState, verdictGreen bool) (draft bool, reason Reason) {
	var closed, unresolved []string
	for _, g := range gating {
		switch prs[g.Branch] {
		case Merged:
		case Closed:
			closed = append(closed, g.URL)
		default: // Open or Absent
			unresolved = append(unresolved, g.URL)
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

func unresolvedGateReason(tickets []string) Reason {
	if len(tickets) == 1 {
		return Reason(fmt.Sprintf("waiting on %s", tickets[0]))
	}
	return Reason(fmt.Sprintf("waiting on %d blockers: %s", len(tickets), strings.Join(tickets, ", ")))
}

func closedGateReason(tickets []string) Reason {
	if len(tickets) == 1 {
		return Reason(fmt.Sprintf("waiting on %s: its pull request was closed without merging", tickets[0]))
	}
	return Reason(fmt.Sprintf("waiting on %d blockers whose pull requests closed without merging: %s",
		len(tickets), strings.Join(tickets, ", ")))
}
