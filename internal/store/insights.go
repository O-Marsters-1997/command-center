package store

import (
	"context"
	"fmt"

	"github.com/O-Marsters-1997/command-center/internal/plan/insight"
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
