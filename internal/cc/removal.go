package cc

import (
	"context"
	"fmt"
)

// RemovalRefusals returns each ticket's most recent remove-worktree refusal, keyed by ticket
// URL -- the row's own removal warning. A successful removal withdraws the ticket, so a stale
// refusal from before a fix can never outlive the row it warns about.
func (s *Store) RemovalRefusals(ctx context.Context) (map[string]string, error) {
	rows, err := s.q.LatestRemovalRefusals(ctx, eventRemoveWorktreeRefused)
	if err != nil {
		return nil, fmt.Errorf("select removal refusals: %w", err)
	}

	refusals := map[string]string{}
	for _, row := range rows {
		if !row.TicketID.Valid {
			continue
		}
		refusals[row.TicketID.String] = row.Detail.String
	}
	return refusals, nil
}
