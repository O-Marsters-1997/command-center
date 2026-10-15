package store

import (
	"context"
	"fmt"
	"time"

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
