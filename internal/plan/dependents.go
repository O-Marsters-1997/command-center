package plan

import "slices"

// Unlocks inverts BlockedBy: each blocker's URL maps to the tickets waiting on it, in input
// order. A ticket nothing waits on has no entry.
func Unlocks(tickets []Ticket) map[string][]string {
	out := make(map[string][]string)
	for _, t := range tickets {
		for _, blocker := range t.BlockedBy {
			if !slices.Contains(out[blocker], t.URL) {
				out[blocker] = append(out[blocker], t.URL)
			}
		}
	}
	return out
}
