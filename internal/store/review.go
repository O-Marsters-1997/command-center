package store

import (
	"context"
	"encoding/json"
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

func (s *Store) RequestChanges(
	ctx context.Context, ticket plan.TicketURL, payload FollowUpPayload, at time.Time,
) error {
	encoded, err := payload.Encode()
	if err != nil {
		return fmt.Errorf("request changes on %s: %w", ticket, err)
	}
	raw, err := json.Marshal(encoded)
	if err != nil {
		return fmt.Errorf("encode follow-up intent payload for %s: %w", ticket, err)
	}
	err = s.inTx(ctx, func(q *ccdb.Queries) error {
		if err := q.DeleteReviewComments(ctx, string(ticket)); err != nil {
			return fmt.Errorf("delete review comments: %w", err)
		}
		if _, err := q.QueueVerbIntent(ctx, ccdb.QueueVerbIntentParams{
			At: at.UTC(), TicketID: string(ticket), Verb: string(plan.VerbFollowUp), Payload: raw,
			QueuedBy: string(plan.QueuedByOwner),
		}); err != nil {
			return fmt.Errorf("queue follow-up intent: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("request changes on %s: %w", ticket, err)
	}
	return nil
}

func (s *Store) Raise(ctx context.Context, ticket plan.TicketURL, tip plan.SHA, at time.Time) (int64, error) {
	var id int64
	err := s.inTx(ctx, func(q *ccdb.Queries) error {
		if err := q.DeleteReviewComments(ctx, string(ticket)); err != nil {
			return fmt.Errorf("delete review comments: %w", err)
		}
		var err error
		id, err = queueVerbIntent(ctx, q, VerbRequest{
			Ticket: string(ticket), Verb: plan.VerbRaise, Payload: string(tip), At: at,
		})
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("raise %s: %w", ticket, err)
	}
	return id, nil
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
