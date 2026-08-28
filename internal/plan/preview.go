package plan

import "fmt"

// PreviewLabel is what a launch preview row shows for a ticket: whether it would start now, on
// unlock, or not at all within the requested slice.
type PreviewLabel int

const (
	Now PreviewLabel = iota
	OnUnlock
	Refused
)

func (l PreviewLabel) String() string {
	switch l {
	case Now:
		return "now"
	case OnUnlock:
		return "on unlock"
	case Refused:
		return "refused"
	default:
		return "refused"
	}
}

// Preview labels one ticket's row in a launch preview: unlocked tickets start now; a locked ticket
// starts on unlock only if every blocker is itself in the requested slice — otherwise nothing
// in this launch will ever satisfy it, and the row is refused (docs/prds/prd-command-centre.md § A launch).
func Preview(unlock Unlock, slice map[string]bool, activeLaunchID int64) (PreviewLabel, Reason) {
	if activeLaunchID != 0 {
		return Refused, Reason(fmt.Sprintf("already authorised in launch %d", activeLaunchID))
	}
	if unlock.Unlocked {
		return Now, unlock.Reason
	}
	for _, blocker := range unlock.Blocking {
		if !slice[blocker] {
			return Refused, Reason(fmt.Sprintf(
				"blocked by %s, which has no open or merged pull request outside this slice", blocker))
		}
	}
	return OnUnlock, unlock.Reason
}

// ProspectiveBase is the base an OnUnlock row would get once unlocked, computed without an
// existing PR — the same selection rule Unlocked applies to its single-blocker, open-PR arm.
// Kept separate from Unlock.BaseBranch, which must stay empty for a blocked row (golden-tested main page).
func ProspectiveBase(t Ticket, byURL map[string]Ticket, stacking bool) string {
	var sameRepo []Ticket
	for _, blockerURL := range t.BlockedBy {
		blocker, ok := byURL[blockerURL]
		if ok && blocker.Repo == t.Repo {
			sameRepo = append(sameRepo, blocker)
		}
	}
	if len(sameRepo) == 1 && stacking {
		return sameRepo[0].Branch
	}
	return defaultBranch
}
