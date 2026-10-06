package store

import (
	"context"
	"fmt"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/store/ccdb"
)

// TicketSpend is one merged ticket's weight: every run disposed before its pr_merged event,
// summed by kind, in dollars.
type TicketSpend struct {
	Ticket      string
	Title       string
	MergedAt    time.Time
	AgentUSD    float64
	ResolveUSD  float64
	FollowUpUSD float64
}

// TotalUSD is every run's weight regardless of kind.
func (t TicketSpend) TotalUSD() float64 {
	return t.AgentUSD + t.ResolveUSD + t.FollowUpUSD
}

// MergedTicketSpend returns one point per ticket whose pr_merged event falls in [since, until],
// weighing every run disposed before that event.
func (s *Store) MergedTicketSpend(
	ctx context.Context, repo, feature, tz string, since, until time.Time,
) ([]TicketSpend, error) {
	rows, err := s.q.MergedTicketSpend(ctx, ccdb.MergedTicketSpendParams{
		Repo: repo, Feature: feature, Timezone: tz, Since: since, Until: until,
	})
	if err != nil {
		return nil, fmt.Errorf("select merged ticket spend: %w", err)
	}

	points := make([]TicketSpend, len(rows))
	for i, row := range rows {
		points[i] = TicketSpend{
			Ticket: row.TicketID, Title: row.Title, MergedAt: row.MergedAt,
			AgentUSD: row.AgentUsd, ResolveUSD: row.ResolveUsd, FollowUpUSD: row.FollowUpUsd,
		}
	}
	return points, nil
}

// BoardTicketSpend is one ticket's own weight right now, merged or not.
type BoardTicketSpend struct {
	AgentUSD, ResolveUSD, FollowUpUSD float64
	Merged                            bool
}

// TotalUSD is every run's weight regardless of kind.
func (t BoardTicketSpend) TotalUSD() float64 {
	return t.AgentUSD + t.ResolveUSD + t.FollowUpUSD
}

// BoardTicketSpend returns every ticket in scope's own weight, keyed by ticket URL, weighing every
// run disposed before its own pr_merged event when merged, or every run disposed so far when
// still open.
func (s *Store) BoardTicketSpend(ctx context.Context, repo, feature string) (map[string]BoardTicketSpend, error) {
	rows, err := s.q.BoardTicketSpend(ctx, ccdb.BoardTicketSpendParams{Repo: repo, Feature: feature})
	if err != nil {
		return nil, fmt.Errorf("select ticket spend: %w", err)
	}

	byURL := make(map[string]BoardTicketSpend, len(rows))
	for _, row := range rows {
		byURL[row.TicketID] = BoardTicketSpend{
			AgentUSD: row.AgentUsd, ResolveUSD: row.ResolveUsd, FollowUpUSD: row.FollowUpUsd, Merged: row.Merged,
		}
	}
	return byURL, nil
}

// WithdrawnTicketWaste sums every disposed run belonging to a ticket withdrawn, by its own
// withdrawal time, without ever merging.
func (s *Store) WithdrawnTicketWaste(
	ctx context.Context, repo, feature, tz string, since, until time.Time,
) (float64, error) {
	usd, err := s.q.WithdrawnTicketWaste(ctx, ccdb.WithdrawnTicketWasteParams{
		Repo: repo, Feature: feature, Timezone: tz, Since: since, Until: until,
	})
	if err != nil {
		return 0, fmt.Errorf("select withdrawn ticket waste: %w", err)
	}
	return usd, nil
}
