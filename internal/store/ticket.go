package store

import (
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/tracker"
)

// Ticket is a tracked issue; tracker fields refresh on import while branch and blockers are seeded once.
type Ticket struct {
	URL       string
	Repo      string
	Branch    string
	BlockedBy []string
	Source    string
	Title     string
	Body      string
	Status    string
	Feature   string
	SyncedAt  time.Time
}

func (t Ticket) Ref() plan.BranchRef { return plan.BranchRef{Repo: t.Repo, Branch: t.Branch} }

func (t Ticket) Plan() plan.Ticket {
	blockedBy := make([]plan.TicketURL, len(t.BlockedBy))
	for i, url := range t.BlockedBy {
		blockedBy[i] = plan.TicketURL(url)
	}
	return plan.Ticket{URL: plan.TicketURL(t.URL), Repo: t.Repo, Branch: t.Branch, BlockedBy: blockedBy}
}

// ImportedTicket associates a tracker ticket with its repo.
type ImportedTicket struct {
	tracker.Ticket
	Repo   string
	Source string
}

// TickError holds the last failed tick and its age.
type TickError struct {
	At      time.Time `json:"at"`
	Message string    `json:"message"`
}

// TickPeriod separates non-overlapping ticks and determines verdict checking time.
const TickPeriod = 15 * time.Second
