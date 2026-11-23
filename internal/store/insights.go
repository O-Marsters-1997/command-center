package store

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/plan/insight"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	"github.com/O-Marsters-1997/command-center/internal/store/ccdb"
)

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

func (s *Store) ConflictFacts(ctx context.Context, q SpendQuery) (insight.ConflictFacts, error) {
	repo, feature, tz := nullIfEmpty(q.Repo), nullIfEmpty(q.Feature), q.timezone()
	rows, err := s.q.ConflictPathEvents(ctx, ccdb.ConflictPathEventsParams{
		Repo: repo, Feature: feature, Timezone: tz, Since: q.Since, Until: q.Until,
	})
	if err != nil {
		return insight.ConflictFacts{}, fmt.Errorf("select conflict paths: %w", err)
	}
	spans, err := s.q.ConflictOpenSpans(ctx, ccdb.ConflictOpenSpansParams{
		Repo: repo, Feature: feature, Timezone: tz, Since: q.Since, Until: q.Until,
	})
	if err != nil {
		return insight.ConflictFacts{}, fmt.Errorf("select conflict open spans: %w", err)
	}

	var facts insight.ConflictFacts
	var lastEvent int64
	for _, row := range rows {
		if len(facts.Conflicts) == 0 || row.EventID != lastEvent {
			lastEvent = row.EventID
			facts.Conflicts = append(facts.Conflicts, insight.ConflictFact{
				Ticket: row.TicketID, Repo: row.Repo, Feature: row.Feature, At: row.At,
			})
		}
		last := &facts.Conflicts[len(facts.Conflicts)-1]
		last.Paths = append(last.Paths, insight.ConflictPath{Path: row.Path, Generated: row.Generated})
	}
	for _, row := range spans {
		span := insight.OpenSpan{Ticket: row.TicketID, Feature: row.Feature, From: row.FromAt}
		if row.ToAt.Valid {
			span.To = option.Some(row.ToAt.Time)
		}
		facts.Spans = append(facts.Spans, span)
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
