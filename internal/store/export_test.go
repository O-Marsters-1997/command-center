package store

import (
	"context"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/store/ccdb"
)

func (s *Store) RunIDsForTicket(ctx context.Context, ticketID string) ([]int64, error) {
	return s.q.RunIDsForTicket(ctx, notNull(ticketID))
}

func (s *Store) CreateProjectThenCancel(ctx context.Context, cancel context.CancelFunc, fnErr error) error {
	return s.inTx(ctx, func(q *ccdb.Queries) error {
		_, err := q.CreateProject(ctx, ccdb.CreateProjectParams{
			Name: "partial", Hue: MinHue, CreatedAt: time.Now().UTC(),
		})
		if err != nil {
			return err
		}
		cancel()
		return fnErr
	})
}
