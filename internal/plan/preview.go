package plan

import "fmt"

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

// Preview labels one ticket's row in a launch preview. A locked ticket starts on unlock only if
// every blocker is in the requested slice, and a non-empty conflictedBase refuses whatever the
// blockers say.
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
// existing PR by the rule Unlocked applies to its single-blocker, open-PR arm.
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

type PreviewRow struct {
	Ticket     Ticket
	Label      PreviewLabel
	Reason     Reason
	Base       string
	BaseRun    *RunFact
	Prompt     string
	PromptHash string
}

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
