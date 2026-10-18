package plan

type LaunchCandidate struct {
	URL               TicketURL
	Unlock            Unlock
	Authorised        bool
	PromptHashMatches bool
	HasRun            bool
	ConflictedBase    string
	Next              Next
	InFlight          bool
}

func (c LaunchCandidate) eligible() bool {
	if c.InFlight {
		return true
	}
	return c.Unlock.Unlocked && c.Authorised && c.PromptHashMatches && !c.HasRun && c.ConflictedBase == ""
}

func LaunchPlan(candidates []LaunchCandidate, currentlyRunning, maxAgents int, spendPaused bool) []TicketURL {
	if spendPaused {
		return nil
	}
	free := maxAgents - currentlyRunning
	if free <= 0 {
		return nil
	}

	var selected []TicketURL
	for _, inFlight := range []bool{true, false} {
		for _, c := range candidates {
			if len(selected) >= free {
				return selected
			}
			if c.InFlight == inFlight && c.eligible() {
				selected = append(selected, c.URL)
			}
		}
	}
	return selected
}
