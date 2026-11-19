package plan

import (
	"maps"
	"slices"

	"github.com/O-Marsters-1997/command-center/internal/plan/verdict"
)

type Rerun struct {
	Ticket     TicketURL
	DetailsURL string
}

func (s Snapshot) Reruns() []Rerun {
	var out []Rerun
	for _, e := range s.Entries {
		pr := s.obs.PRs[e.Ticket.Ref()]
		if pr.State != Open {
			continue
		}
		for _, name := range slices.Sorted(maps.Keys(pr.Checks)) {
			c := pr.Checks[name]
			if c.Attempt == 1 && ToVerdictCheckState(c) == verdict.Failure {
				out = append(out, Rerun{Ticket: e.Ticket.URL, DetailsURL: c.DetailsURL})
			}
		}
	}
	return out
}
