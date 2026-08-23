package plan

import "fmt"

// PreviewLabel is what a launch preview row shows for a task: whether it would start now, on
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

// Preview labels one task's row in a launch preview: already unlocked tasks start now; a
// locked task starts on unlock only if every one of its blockers is itself in the requested
// slice — otherwise there is nothing in this launch that will ever satisfy it, and the row is
// refused, naming the blocker outside the slice (docs/prd-command-centre.md § A launch).
func Preview(unlock Unlock, slice map[string]bool) (PreviewLabel, Reason) {
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
// existing PR — the same selection rule Unlocked applies to its single-blocker, open-PR arm,
// since that is how such a row actually unlocks. Kept separate from Unlock.BaseBranch, which
// must stay empty for a blocked row so the golden-tested main page is untouched by this.
func ProspectiveBase(t Task, byURL map[string]Task, stacking bool) string {
	var sameRepo []Task
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
