package plan

type LaunchCandidate struct {
	URL               string
	Unlock            Unlock
	Authorised        bool
	PromptHashMatches bool
	HasRun            bool
	ConflictedBase    string
}

func (c LaunchCandidate) eligible() bool {
	return c.Unlock.Unlocked && c.Authorised && c.PromptHashMatches && !c.HasRun && c.ConflictedBase == ""
}

// LaunchPlan selects the ticket URLs to cut and spawn: every eligible candidate in input order,
// capped at maxAgents minus currentlyRunning. spendPaused stops every new spawn and leaves live
// runs alone.
func LaunchPlan(candidates []LaunchCandidate, currentlyRunning, maxAgents int, spendPaused bool) []string {
	if spendPaused {
		return nil
	}
	free := maxAgents - currentlyRunning
	if free <= 0 {
		return nil
	}

	var selected []string
	for _, c := range candidates {
		if len(selected) >= free {
			break
		}
		if c.eligible() {
			selected = append(selected, c.URL)
		}
	}
	return selected
}
