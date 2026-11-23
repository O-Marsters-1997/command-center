package store

import (
	"context"
	"encoding/json"
	"fmt"

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
