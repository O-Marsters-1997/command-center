package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/store/ccdb"
)

func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) TestCancelInTx(ctx context.Context, cancel context.CancelFunc, wantErr error) error {
	return s.inTx(ctx, func(q *ccdb.Queries) error {
		_, err := q.CreateProject(ctx, ccdb.CreateProjectParams{
			Name: "partial", Hue: MinHue, CreatedAt: time.Now().UTC(),
		})
		if err != nil {
			return err
		}
		cancel()
		return wantErr
	})
}
