package plan

import "slices"

func Unlocks(tickets []Ticket) map[TicketURL][]TicketURL {
	out := make(map[TicketURL][]TicketURL)
	for _, t := range tickets {
		for _, blocker := range t.BlockedBy {
			if !slices.Contains(out[blocker], t.URL) {
				out[blocker] = append(out[blocker], t.URL)
			}
		}
	}
	return out
}
