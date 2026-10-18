package store

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
)

type Disposition struct {
	RunID       plan.RunID
	Outcome     plan.Outcome
	ExitCode    option.Option[int]
	Cost        option.Option[float64]
	LastMessage option.Option[string]
	Metrics     option.Option[agentlog.RunMetrics]
	EndedAt     time.Time
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
	Choice      plan.AgentChoice
	Session     RunSession

	Step         plan.StepName
	Workflow     string
	WorkflowHash string
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
	Context    string
	Choice     plan.AgentChoice
	At         time.Time
}

type VerbRequest struct {
	Ticket  string
	Verb    plan.Verb
	Payload string
	At      time.Time
}

type FollowUpPayload struct {
	Message string      `json:"message"`
	Model   string      `json:"model"`
	Effort  plan.Effort `json:"effort,omitempty"`
}

func (p FollowUpPayload) Encode() (string, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return "", fmt.Errorf("encode follow-up payload: %w", err)
	}
	return string(raw), nil
}

func DecodeFollowUpPayload(raw string) FollowUpPayload {
	var p FollowUpPayload
	if strings.HasPrefix(raw, "{") && json.Unmarshal([]byte(raw), &p) == nil {
		return p
	}
	return FollowUpPayload{Message: raw}
}

type EditRequest struct {
	Ticket    string
	Branch    string
	BlockedBy []string
	At        time.Time
}
