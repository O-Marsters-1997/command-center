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
	RedBlocker        TicketURL
}

func (c LaunchCandidate) eligible() bool {
	if c.InFlight {
		return true
	}
	return c.Unlock.Unlocked && c.Authorised && c.PromptHashMatches && !c.HasRun &&
		c.ConflictedBase == "" && c.RedBlocker == ""
}

func (c LaunchCandidate) command() bool {
	return c.Next.Kind == NextRun && c.Next.Step.IsSome() && c.Next.Step.Unwrap().Command
}

func LaunchPlan(candidates []LaunchCandidate, load Load, caps Caps, spendPaused bool) []TicketURL {
	if spendPaused {
		return nil
	}

	var selected []TicketURL
	for _, inFlight := range []bool{true, false} {
		for _, c := range candidates {
			if c.InFlight != inFlight || !c.eligible() || !load.fits(c.command(), caps) {
				continue
			}
			selected = append(selected, c.URL)
			load = load.charge(c.command())
		}
	}
	return selected
}
