package store

import (
	"context"
	"fmt"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

// RemovalRefusals returns each ticket's latest remove-worktree refusal.
func (s *Store) RemovalRefusals(ctx context.Context) (map[plan.TicketURL]string, error) {
	rows, err := s.q.ListLatestRemovalRefusals(ctx, string(EventRemoveWorktreeRefused))
	if err != nil {
		return nil, fmt.Errorf("select removal refusals: %w", err)
	}

	refusals := map[plan.TicketURL]string{}
	for _, row := range rows {
		refusals[plan.TicketURL(row.TicketID)] = row.Detail
	}
	return refusals, nil
}
