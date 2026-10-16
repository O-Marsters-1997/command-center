package store

import (
	"context"
	"fmt"
	"time"
)

type Activity struct {
	TicketURL   string
	At          time.Time
	LastMessage string
}

func (s *Store) Activities(ctx context.Context) ([]Activity, error) {
	rows, err := s.q.TicketActivities(ctx)
	if err != nil {
		return nil, fmt.Errorf("select ticket activities: %w", err)
	}
	out := make([]Activity, 0, len(rows))
	for _, row := range rows {
		out = append(out, Activity{TicketURL: row.TicketUrl, At: row.At, LastMessage: row.LastMessage})
	}
	return out, nil
}
