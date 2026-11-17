package store

import (
	"context"
	"fmt"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store/ccdb"
)

type NewReviewComment struct {
	Ticket plan.TicketURL
	plan.ReviewComment
	At time.Time
}

func (s *Store) AddReviewComment(ctx context.Context, c NewReviewComment) (int64, error) {
	id, err := s.q.AddReviewComment(ctx, ccdb.AddReviewCommentParams{
		TicketID:  string(c.Ticket),
		Tip:       string(c.Tip),
		Path:      c.Path,
		Line:      int32(c.Line),
		Side:      string(c.Side),
		Hunk:      c.Hunk,
		Body:      c.Body,
		CreatedAt: c.At.UTC(),
	})
	if err != nil {
		return 0, fmt.Errorf("add review comment on %s: %w", c.Ticket, err)
	}
	return id, nil
}

func (s *Store) DeleteReviewComment(ctx context.Context, ticket plan.TicketURL, id int64) error {
	if err := s.q.DeleteReviewComment(ctx, ccdb.DeleteReviewCommentParams{TicketID: string(ticket), ID: id}); err != nil {
		return fmt.Errorf("delete review comment %d on %s: %w", id, ticket, err)
	}
	return nil
}

func (s *Store) ReviewComments(ctx context.Context, ticket plan.TicketURL) ([]plan.ReviewComment, error) {
	rows, err := s.q.ListReviewComments(ctx, string(ticket))
	if err != nil {
		return nil, fmt.Errorf("select review comments for %s: %w", ticket, err)
	}
	comments := make([]plan.ReviewComment, len(rows))
	for i, r := range rows {
		comments[i] = plan.ReviewComment{
			ID: r.ID, Tip: plan.SHA(r.Tip), Path: r.Path, Line: int(r.Line), Side: plan.Side(r.Side), Hunk: r.Hunk, Body: r.Body,
		}
	}
	return comments, nil
}
