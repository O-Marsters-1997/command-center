package store

import (
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/tracker"
)

// Ticket is one tracked issue. Source, Title, Body, Status, Feature and SyncedAt are the
// tracker's, refreshed on every import; Branch and BlockedBy are the app's, seeded once.
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
	SyncedAt  string
}

func (t Ticket) Plan() plan.Ticket {
	return plan.Ticket{URL: t.URL, Repo: t.Repo, Branch: t.Branch, BlockedBy: t.BlockedBy}
}

// ImportedTicket is one tracker.Ticket with the tracked repo it came from.
type ImportedTicket struct {
	tracker.Ticket
	Repo   string
	Source string
}

// TickError is the last failed tick, with its age shown on the page.
type TickError struct {
	At      time.Time `json:"at"`
	Message string    `json:"message"`
}

// TickPeriod is the sleep after the loop's work; ticks never overlap. A verdict's checking
// time is counted in these.
const TickPeriod = 15 * time.Second
