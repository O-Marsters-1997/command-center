package plan

import "fmt"

func GatingBlockers(t Ticket, byURL map[TicketURL]Ticket) []Ticket {
	var gating []Ticket
	for _, blockerURL := range t.BlockedBy {
		blocker, ok := byURL[blockerURL]
		if ok && blocker.Repo != t.Repo {
			gating = append(gating, blocker)
		}
	}
	return gating
}

func DraftGate(gating []Ticket, prs map[string]PRState, verdictGreen bool) (draft bool, reason Reason) {
	var closed, unresolved []TicketURL
	for _, g := range gating {
		switch prs[g.Branch] {
		case Merged:
		case Closed:
			closed = append(closed, g.URL)
		case Open, Absent:
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

func unresolvedGateReason(tickets []TicketURL) Reason {
	if len(tickets) == 1 {
		return Reason(fmt.Sprintf("waiting on %s", tickets[0]))
	}
	return Reason(fmt.Sprintf("waiting on %d blockers: %s", len(tickets), joinURLs(tickets)))
}

func closedGateReason(tickets []TicketURL) Reason {
	if len(tickets) == 1 {
		return Reason(fmt.Sprintf("waiting on %s: its pull request was closed without merging", tickets[0]))
	}
	return Reason(fmt.Sprintf("waiting on %d blockers whose pull requests closed without merging: %s",
		len(tickets), joinURLs(tickets)))
}
