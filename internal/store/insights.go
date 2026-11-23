package store

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/plan/insight"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	"github.com/O-Marsters-1997/command-center/internal/store/ccdb"
)

// TicketSpend splits a merged ticket's dollar weight by run kind.
type TicketSpend struct {
	Ticket      string
	Title       string
	Feature     string
	MergedAt    time.Time
	AgentUSD    float64
	FollowUpUSD float64
}

func (t TicketSpend) TotalUSD() float64 {
	return t.AgentUSD + t.FollowUpUSD
}

// MergedTicketSpend includes runs disposed before each merge in the inclusive time range.
func (s *Store) MergedTicketSpend(ctx context.Context, q SpendQuery) ([]TicketSpend, error) {
	rows, err := s.q.MergedTicketSpend(ctx, ccdb.MergedTicketSpendParams{
		Repo: nullIfEmpty(q.Repo), Feature: nullIfEmpty(q.Feature), Timezone: q.timezone(), Since: q.Since, Until: q.Until,
	})
	if err != nil {
		return nil, fmt.Errorf("select merged ticket spend: %w", err)
	}

	points := make([]TicketSpend, len(rows))
	for i, row := range rows {
		points[i] = TicketSpend{
			Ticket: row.TicketID, Title: row.Title, Feature: row.Feature, MergedAt: row.MergedAt,
			AgentUSD: row.AgentUsd, FollowUpUSD: row.FollowUpUsd,
		}
	}
	return points, nil
}

func (s *Store) MergedTicketFacts(ctx context.Context, q SpendQuery) ([]insight.TicketFact, error) {
	rows, err := s.q.MergedTicketFacts(ctx, ccdb.MergedTicketFactsParams{
		Repo: nullIfEmpty(q.Repo), Feature: nullIfEmpty(q.Feature), Timezone: q.timezone(), Since: q.Since, Until: q.Until,
	})
	if err != nil {
		return nil, fmt.Errorf("select merged ticket facts: %w", err)
	}

	facts := make([]insight.TicketFact, len(rows))
	for i, row := range rows {
		facts[i] = insight.TicketFact{
			Ticket: row.TicketID, Title: row.Title, Repo: row.Repo, Feature: row.Feature,
			FirstRunAt: row.FirstRunAt, MergedAt: row.MergedAt,
			WeightUSD: row.WeightUsd, Tokens: row.Tokens, ToolCalls: int(row.ToolCalls),
		}
	}
	return facts, nil
}

func (s *Store) TicketInsightFacts(ctx context.Context, ticket string) (insight.TicketFacts, error) {
	key := nullIfEmpty(ticket)
	runRows, err := s.q.TicketInsightRuns(ctx, key)
	if err != nil {
		return insight.TicketFacts{}, fmt.Errorf("select ticket runs: %w", err)
	}
	pushRows, err := s.q.TicketInsightPushes(ctx, key)
	if err != nil {
		return insight.TicketFacts{}, fmt.Errorf("select ticket pushes: %w", err)
	}
	merged, err := s.q.TicketMergedAt(ctx, key)
	if err != nil {
		return insight.TicketFacts{}, fmt.Errorf("select ticket merge: %w", err)
	}

	var facts insight.TicketFacts
	for _, row := range runRows {
		var m agentlog.RunMetrics
		if err := json.Unmarshal(row.Metrics, &m); err != nil {
			return insight.TicketFacts{}, fmt.Errorf("decode metrics of run %d: %w", row.ID, err)
		}
		facts.Runs = append(facts.Runs, insight.RunFact{
			ID: row.ID, Step: row.Step, Kind: row.Kind,
			StartedAt: row.ProcStartedAt.Time, EndedAt: row.EndedAt.Time,
			WeightUSD: row.CostUsd, Tokens: m.Input + m.CacheWrite + m.Output, Tools: m.Tools, Outcome: row.Outcome,
		})
	}
	for _, row := range pushRows {
		facts.Pushes = append(facts.Pushes, insight.PushFact{At: row.PushedAt, Verdict: insight.PushVerdict(row.Verdict)})
	}
	if len(merged) > 0 {
		facts.MergedAt = option.Some(merged[0])
	}
	return facts, nil
}

// BoardTicketSpend holds a ticket's current weight, merged or not.
type BoardTicketSpend struct {
	AgentUSD, FollowUpUSD float64
	Merged                bool
}

func (t BoardTicketSpend) TotalUSD() float64 {
	return t.AgentUSD + t.FollowUpUSD
}

// BoardTicketSpend includes runs disposed before merge for merged tickets and all disposed runs for open ones.
func (s *Store) BoardTicketSpend(ctx context.Context, repo, feature string) (map[string]BoardTicketSpend, error) {
	rows, err := s.q.BoardTicketSpend(ctx, ccdb.BoardTicketSpendParams{
		Repo:    nullIfEmpty(repo),
		Feature: nullIfEmpty(feature),
	})
	if err != nil {
		return nil, fmt.Errorf("select ticket spend: %w", err)
	}

	byURL := make(map[string]BoardTicketSpend, len(rows))
	for _, row := range rows {
		byURL[row.TicketID] = BoardTicketSpend{
			AgentUSD: row.AgentUsd, FollowUpUSD: row.FollowUpUsd,
			Merged: row.Merged,
		}
	}
	return byURL, nil
}

// WithdrawnTicketWaste excludes tickets withdrawn after merging.
func (s *Store) WithdrawnTicketWaste(ctx context.Context, q SpendQuery) (float64, error) {
	usd, err := s.q.WithdrawnTicketWaste(ctx, ccdb.WithdrawnTicketWasteParams{
		Repo: nullIfEmpty(q.Repo), Feature: nullIfEmpty(q.Feature), Timezone: q.timezone(), Since: q.Since, Until: q.Until,
	})
	if err != nil {
		return 0, fmt.Errorf("select withdrawn ticket waste: %w", err)
	}
	return usd, nil
}

// StepSpend is a merged ticket's dollar weight at one step, with the frozen
// metrics of its measured runs summed. Runs disposed before metrics were
// frozen add to WeightUSD only; Metrics.Wall is None when none were measured.
type StepSpend struct {
	Step      plan.StepName
	WeightUSD float64
	Metrics   agentlog.RunMetrics
}

// WorkflowTicketSpend is a merged ticket's spend by step, under the workflow of its first run.
type WorkflowTicketSpend struct {
	Ticket       string
	MergedAt     time.Time
	Workflow     string
	WorkflowHash string
	Steps        []StepSpend
	Rework       Rework
}

type Rework struct {
	FollowUps int
	ReRuns    int
	RedCI     int
}

func (r Rework) Total() int {
	return r.FollowUps + r.ReRuns + r.RedCI
}

// MergedTicketWorkflowSpend covers every run a ticket needed until pr_merged, follow-ups included.
func (s *Store) MergedTicketWorkflowSpend(ctx context.Context, q SpendQuery) ([]WorkflowTicketSpend, error) {
	rows, err := s.q.MergedTicketRuns(ctx, ccdb.MergedTicketRunsParams{
		Repo: nullIfEmpty(q.Repo), Feature: nullIfEmpty(q.Feature), Timezone: q.timezone(), Since: q.Since, Until: q.Until,
	})
	if err != nil {
		return nil, fmt.Errorf("select merged ticket runs: %w", err)
	}

	reworkRows, err := s.q.MergedTicketRework(ctx, ccdb.MergedTicketReworkParams{
		Repo: nullIfEmpty(q.Repo), Feature: nullIfEmpty(q.Feature), Timezone: q.timezone(), Since: q.Since, Until: q.Until,
	})
	if err != nil {
		return nil, fmt.Errorf("select merged ticket rework: %w", err)
	}
	rework := make(map[string]Rework, len(reworkRows))
	for _, row := range reworkRows {
		rework[row.TicketID] = Rework{FollowUps: int(row.FollowUps), ReRuns: int(row.ReRuns), RedCI: int(row.RedCi)}
	}

	var tickets []WorkflowTicketSpend
	for _, row := range rows {
		if n := len(tickets); n == 0 || tickets[n-1].Ticket != row.TicketID {
			tickets = append(tickets, WorkflowTicketSpend{
				Ticket: row.TicketID, MergedAt: row.MergedAt, Workflow: row.Workflow, WorkflowHash: row.WorkflowHash,
				Rework: rework[row.TicketID],
			})
		}
		ticket := &tickets[len(tickets)-1]
		i := slices.IndexFunc(ticket.Steps, func(s StepSpend) bool { return s.Step == plan.StepName(row.Step) })
		if i < 0 {
			ticket.Steps = append(ticket.Steps, StepSpend{Step: plan.StepName(row.Step)})
			i = len(ticket.Steps) - 1
		}
		step := &ticket.Steps[i]
		step.WeightUSD += row.CostUsd
		if !row.Measured {
			continue
		}
		var m agentlog.RunMetrics
		if err := json.Unmarshal(row.Metrics, &m); err != nil {
			return nil, fmt.Errorf("decode metrics of %s: %w", row.TicketID, err)
		}
		step.Metrics = addMetrics(step.Metrics, m)
	}
	return tickets, nil
}

func (s *Store) WithdrawnWorkflowTickets(ctx context.Context, q SpendQuery) (map[string]int, error) {
	rows, err := s.q.WithdrawnWorkflowTickets(ctx, ccdb.WithdrawnWorkflowTicketsParams{
		Repo: nullIfEmpty(q.Repo), Feature: nullIfEmpty(q.Feature), Timezone: q.timezone(), Since: q.Since, Until: q.Until,
	})
	if err != nil {
		return nil, fmt.Errorf("select withdrawn workflow tickets: %w", err)
	}
	byHash := make(map[string]int, len(rows))
	for _, row := range rows {
		byHash[row.WorkflowHash] = int(row.Withdrawn)
	}
	return byHash, nil
}

type landedCheck struct {
	at     time.Time
	name   string
	failed bool
}

func (s *Store) ReworkFacts(ctx context.Context, q SpendQuery) (insight.ReworkFacts, error) {
	repo, feature, tz := nullIfEmpty(q.Repo), nullIfEmpty(q.Feature), q.timezone()
	var facts insight.ReworkFacts

	parks, err := s.q.ParkEvents(ctx, ccdb.ParkEventsParams{
		Repo: repo, Feature: feature, Timezone: tz, Since: q.Since, Until: q.Until,
	})
	if err != nil {
		return insight.ReworkFacts{}, fmt.Errorf("select park events: %w", err)
	}
	for _, p := range parks {
		facts.Parks = append(facts.Parks, insight.ParkFact{
			Ticket: p.TicketID, Repo: p.Repo, Reason: insight.ParkReason(p.Detail), Detail: p.Detail,
			At: p.At, Tokens: p.Tokens,
		})
	}

	landed, err := s.q.ChecksLandedEvents(ctx, ccdb.ChecksLandedEventsParams{
		Repo: repo, Feature: feature, Timezone: tz, Until: q.Until,
	})
	if err != nil {
		return insight.ReworkFacts{}, fmt.Errorf("select landed checks: %w", err)
	}
	loc := cmp.Or(q.Loc, time.UTC)
	sinceY, sinceM, sinceD := q.Since.Date()
	since := time.Date(sinceY, sinceM, sinceD, 0, 0, 0, 0, loc)
	byTicket := make(map[string][]landedCheck)
	for _, e := range landed {
		i := strings.LastIndexByte(e.Detail, ' ')
		if i < 0 {
			continue
		}
		check := landedCheck{at: e.At, name: e.Detail[:i], failed: e.Detail[i+1:] == "failed"}
		byTicket[e.TicketID] = append(byTicket[e.TicketID], check)
		if check.failed && !e.At.Before(since) {
			facts.Failed = append(facts.Failed, insight.FailedCheck{
				Ticket: e.TicketID, Repo: e.Repo, Name: check.name, At: e.At,
			})
		}
	}

	fixes, err := s.q.CIFixRuns(ctx, ccdb.CIFixRunsParams{
		Repo: repo, Feature: feature, Timezone: tz, Since: q.Since, Until: q.Until,
	})
	if err != nil {
		return insight.ReworkFacts{}, fmt.Errorf("select ci fix runs: %w", err)
	}
	for _, f := range fixes {
		latest := make(map[string]bool)
		for _, c := range byTicket[f.TicketID] {
			if c.at.After(f.LaunchedAt) {
				break
			}
			latest[c.name] = c.failed
		}
		var red []string
		for name, failed := range latest {
			if failed {
				red = append(red, name)
			}
		}
		slices.Sort(red)
		facts.Fixes = append(facts.Fixes, insight.FixRun{
			Ticket: f.TicketID, LaunchedAt: f.LaunchedAt, Tokens: f.Tokens, Checks: red,
		})
	}
	return facts, nil
}

func addMetrics(a, b agentlog.RunMetrics) agentlog.RunMetrics {
	switch {
	case a.Wall.IsSome() && b.Wall.IsSome():
		a.Wall = option.Some(a.Wall.Unwrap() + b.Wall.Unwrap())
	case b.Wall.IsSome():
		a.Wall = b.Wall
	}
	a.Turns += b.Turns
	a.Input += b.Input
	a.CacheWrite += b.CacheWrite
	a.CacheRead += b.CacheRead
	a.Output += b.Output
	for tool, n := range b.Tools {
		if a.Tools == nil {
			a.Tools = make(map[string]int)
		}
		a.Tools[tool] += n
	}
	return a
}
