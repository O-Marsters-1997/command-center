package store

import (
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/tracker"
)

// Ticket is one tracked issue. Source, Title, Body, Status, Feature and SyncedAt are the
// tracker's own, refreshed on every import; Repo is matched from URL against a [[repo]]'s remote.
// Branch and BlockedBy are the app's own, seeded once on a URL's first import, then left alone.
// FirstPushCI and HandChurnLines are nil until recordFirstPushCI/recordMergedEvents observe the
// fact they report, and never overwritten after that.
type Ticket struct {
	URL            string
	Repo           string
	Branch         string
	BlockedBy      []string
	Source         string
	Title          string
	Body           string
	Status         string
	Feature        string
	SyncedAt       string
	FirstPushCI    *bool
	HandChurnLines *int
}

// Plan is the slice of t that internal/plan reads.
func (t Ticket) Plan() plan.Ticket {
	return plan.Ticket{URL: t.URL, Repo: t.Repo, Branch: t.Branch, BlockedBy: t.BlockedBy}
}

// ImportedTicket is one tracker.Ticket paired with the configured repo it came from and that
// repo's tracker kind -- Store.ImportTickets's own upsert unit.
type ImportedTicket struct {
	tracker.Ticket
	Repo   string
	Source string
}

// TickError is the last failed tick, rendered on the page with its age.
type TickError struct {
	At      time.Time `json:"at"`
	Message string    `json:"message"`
}

// TickPeriod is the sleep after the loop's work: ticks never overlap, and the loop never branches
// on why it woke. A verdict's checking time is counted in these.
const TickPeriod = 15 * time.Second

// Event kinds both the loop writes and a Store read decodes.
const (
	EventImportRefused         = "import_refused"
	EventRemoveWorktreeRefused = "remove_worktree_refused"
	EventVerdictTransition     = "verdict_transition"
)
