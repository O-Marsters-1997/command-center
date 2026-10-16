package plan

import (
	"errors"
	"fmt"

	"github.com/O-Marsters-1997/command-center/internal/plan/option"
)

// ErrUnknownTicket indicates a ticket absent from the snapshot.
var ErrUnknownTicket = errors.New("unknown ticket")

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

func Preview(
	unlock Unlock, slice map[TicketURL]bool, activeLaunchID int64, conflictedBase string,
) (PreviewLabel, Reason) {
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

func ProspectiveBase(t Ticket, byURL map[TicketURL]Ticket, stacking bool) string {
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
	return DefaultBaseBranch
}

type PreviewRow struct {
	Ticket     Ticket
	Label      PreviewLabel
	Reason     Reason
	Base       string
	BaseRun    option.Option[RunFact]
	Prompt     string
	PromptHash string
}

func (s Snapshot) Preview(selection []TicketURL, launchContext string) ([]PreviewRow, error) {
	entries := make([]Entry, 0, len(selection))
	slice := make(map[TicketURL]bool, len(selection))
	for _, ticketURL := range selection {
		e, ok := s.Entry(ticketURL)
		if !ok {
			return nil, fmt.Errorf("%w %q", ErrUnknownTicket, ticketURL)
		}
		entries = append(entries, e)
		slice[ticketURL] = true
	}

	rows := make([]PreviewRow, 0, len(entries))
	for _, e := range entries {
		label, reason := Preview(e.Unlock, slice, e.LaunchID, e.ConflictedBase)
		launched := e.Ticket
		launched.LaunchContext = launchContext
		prompt := Compose(launched)
		rows = append(rows, PreviewRow{
			Ticket: e.Ticket, Label: label, Reason: reason,
			Base: e.Base, BaseRun: s.baseRun(e.Base),
			Prompt: prompt, PromptHash: Hash(prompt),
		})
	}
	return rows, nil
}

func (s Snapshot) baseRun(base string) option.Option[RunFact] {
	if base == DefaultBaseBranch {
		return option.None[RunFact]()
	}
	for _, e := range s.Entries {
		if e.Ticket.Branch == base {
			return e.Run
		}
	}
	return option.None[RunFact]()
}
