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
// A non-empty conflictedBase refuses whatever the blockers say: nothing is ever cut from a base
// that already carries a conflict (docs/adr/0004-conflicts-resolve-once-and-one-peer-at-a-time.md).
func Preview(unlock Unlock, slice map[string]bool, activeLaunchID int64, conflictedBase string) (PreviewLabel, Reason) {
	if activeLaunchID != 0 {
		return Refused, Reason(fmt.Sprintf("already authorised in launch %d", activeLaunchID))
	}
	if conflictedBase != "" {
		return Refused, conflictedBaseReason(conflictedBase)
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

// PreviewRow is one requested ticket's row in a launch preview. BaseRun is the run of the ticket
// whose branch Base names, nil when the row is cut from main.
type PreviewRow struct {
	Ticket     Ticket
	Label      PreviewLabel
	Reason     Reason
	Base       string
	BaseRun    *RunFact
	Prompt     string
	PromptHash string
}

// Preview is what launching selection would do, row by row in selection order. It errors on a
// ticket the snapshot does not hold.
func (s Snapshot) Preview(selection []string) ([]PreviewRow, error) {
	entries := make([]Entry, 0, len(selection))
	slice := make(map[string]bool, len(selection))
	for _, ticketURL := range selection {
		e, ok := s.Entry(ticketURL)
		if !ok {
			return nil, fmt.Errorf("unknown ticket %q", ticketURL)
		}
		entries = append(entries, e)
		slice[ticketURL] = true
	}

	rows := make([]PreviewRow, 0, len(entries))
	for _, e := range entries {
		label, reason := Preview(e.Unlock, slice, e.LaunchID, e.ConflictedBase)
		prompt := Compose(e.Ticket)
		rows = append(rows, PreviewRow{
			Ticket: e.Ticket, Label: label, Reason: reason,
			Base: e.Base, BaseRun: s.baseRun(e.Base),
			Prompt: prompt, PromptHash: Hash(prompt),
		})
	}
	return rows, nil
}

func (s Snapshot) baseRun(base string) *RunFact {
	if base == defaultBranch {
		return nil
	}
	for _, e := range s.Entries {
		if e.Ticket.Branch == base {
			return e.Run
		}
	}
	return nil
}
