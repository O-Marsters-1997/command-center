package store

import (
	"context"
	"fmt"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

// RemovalRefusals returns each ticket's most recent remove-worktree refusal, keyed by
// ticket URL.
func (s *Store) RemovalRefusals(ctx context.Context) (map[plan.TicketURL]string, error) {
	rows, err := s.q.LatestRemovalRefusals(ctx, string(EventRemoveWorktreeRefused))
	if err != nil {
		return nil, fmt.Errorf("select removal refusals: %w", err)
	}

	refusals := map[plan.TicketURL]string{}
	for _, row := range rows {
		if !row.TicketID.Valid {
			continue
		}
		refusals[plan.TicketURL(row.TicketID.String)] = row.Detail.String
	}
	return refusals, nil
}
