package plan

// LaunchCandidate is one ticket's launch eligibility facts (inv. 8): unlocked, in an active
// launch, whose recomposed prompt still hashes to what was authorised, with no prior run.
type LaunchCandidate struct {
	URL               string
	Unlock            Unlock
	Authorised        bool
	PromptHashMatches bool
	HasRun            bool
}

func (c LaunchCandidate) eligible() bool {
	return c.Unlock.Unlocked && c.Authorised && c.PromptHashMatches && !c.HasRun
}

// LaunchPlan selects the ticket URLs to cut and spawn this tick: every eligible candidate, in
// input order, capped at the number of free agent slots (maxAgents, applied globally, minus
// currentlyRunning).
func LaunchPlan(candidates []LaunchCandidate, currentlyRunning, maxAgents int) []string {
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
