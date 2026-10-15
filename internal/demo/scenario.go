package demo

import "time"

const (
	resultCommits  = "commits"
	resultCrash    = "crash"
	resultConflict = "conflict"
)

// Scenario defines one demo run's repositories, ticket DAG, scripts and external commits.
type Scenario struct {
	Seed    int64
	Repos   []Repo
	Tickets []Ticket
	Main    []Main
}

// Repo defines a sandbox origin seeded with Files on main.
type Repo struct {
	Name        string
	Stacking    bool
	CompatCheck string
	Files       map[string]string
}

// Ticket defines an issue and its scripted agent and CI outcomes.
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

// Main defines an external commit on a repo's origin main at sim time At.
type Main struct {
	At    time.Duration
	Repo  string
	Files map[string]string
}
