package store

import (
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
)

type Disposition struct {
	RunID    plan.RunID
	Outcome  plan.Outcome
	ExitCode option.Option[int]
	Cost     option.Option[float64]
	EndedAt  time.Time
}

type Push struct {
	Ticket     plan.TicketURL
	Tip        plan.SHA
	BaseSHA    plan.SHA
	BaseBranch string
	At         time.Time
}

type SpendQuery struct {
	Repo    string
	Feature string
	Loc     *time.Location
	Since   time.Time
	Until   time.Time
}

func (q SpendQuery) timezone() string {
	if q.Loc == nil {
		return "UTC"
	}
	return q.Loc.String()
}

type RunSkeleton struct {
	Ticket      string
	Kind        plan.RunKind
	BaselineSHA string
	PromptHash  string
	Session     RunSession
}

type Spawn struct {
	RunID     plan.RunID
	Pgid      int
	StartedAt time.Time
	LogPath   string
}

type NewProject struct {
	Name  string
	Hue   int
	Repos []string
	At    time.Time
}

type LaunchRequest struct {
	Ticket     string
	PromptHash string
	Group      string
	At         time.Time
}

type VerbRequest struct {
	Ticket  string
	Verb    plan.Verb
	Payload string
	At      time.Time
}

type EditRequest struct {
	Ticket    string
	Branch    string
	BlockedBy []string
	At        time.Time
}
