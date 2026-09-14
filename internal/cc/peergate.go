package cc

import (
	"sort"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/tracker"
)

// conflictingPeerHold names, for every open main-based ticket, the lower-ref peer its branch
// conflicts with -- keyed by ticket URL, absent when there is none. Candidates are sorted into
// ref order, which this package alone knows, so that of any conflicting pair only the lower one
// proceeds; a stacked branch is never a candidate here, since conflictedBase already owns that
// case (docs/adr/0010-one-conflicting-peer-at-a-time.md).
func conflictingPeerHold(
	tickets []Ticket, byURL map[string]plan.Ticket, prs map[string]plan.PRState,
	stackingByRepo map[string]bool, obs Observation,
) map[string]string {
	var candidates []Ticket
	for _, t := range tickets {
		if prs[t.Branch] != plan.Open {
			continue
		}
		if plan.ProspectiveBase(byURL[t.URL], byURL, stackingByRepo[t.Repo]) != defaultBaseBranch {
			continue
		}
		candidates = append(candidates, t)
	}
	sort.Slice(candidates, func(i, j int) bool { return lessByRef(candidates[i].Branch, candidates[j].Branch) })

	held := make(map[string]string, len(candidates))
	for i, t := range candidates {
		for _, peer := range candidates[:i] {
			if obs.ConflictsWithPeer[t.Branch][peer.Branch] {
				held[t.URL] = peer.Branch
				break
			}
		}
	}
	return held
}

// lessByRef orders two ticket branches by the ticket number tracker.BranchSlug encoded in each,
// so cc-9 sorts before cc-100: a plain string compare would put "cc-100" first the moment ticket
// numbers cross a digit-count boundary. Falls back to a string compare for a branch BranchNumber
// cannot parse, which never happens for a branch this app generated.
func lessByRef(a, b string) bool {
	na, oka := tracker.BranchNumber(a)
	nb, okb := tracker.BranchNumber(b)
	if oka && okb {
		return na < nb
	}
	return a < b
}
