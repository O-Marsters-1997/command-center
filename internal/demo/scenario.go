package demo

import "time"

const (
	resultCommits  = "commits"
	resultCrash    = "crash"
	resultConflict = "conflict"
)

// Scenario is one demo run: the repos, the ticket DAG, each ticket's script and the commits that
// land on main. Every duration is sim time, from the run's start or, where a field says so, from
// the event that starts it.
type Scenario struct {
	Seed    int64
	Repos   []Repo
	Tickets []Ticket
	Main    []Main
}

// Repo is one repository the sandbox hosts a bare origin for, seeded with Files on main.
type Repo struct {
	Name        string
	Stacking    bool
	CompatCheck string
	Files       map[string]string
}

// Ticket is one issue in the DAG and the script the world plays for it. A ticket whose first
// spawn fails succeeds on the next; CI lists each successive check run's outcome.
type Ticket struct {
	ID          string
	Repo        string
	Title       string
	Feature     string
	BlockedBy   []string
	AgentAfter  time.Duration
	Result      string
	Files       []string
	CI          []string
	CIAfter     time.Duration
	MergeAfter  time.Duration
	CompatFails bool
}

// Main is a commit landing on a repo's origin main at sim time At, not from cc.
type Main struct {
	At    time.Duration
	Repo  string
	Files map[string]string
}
