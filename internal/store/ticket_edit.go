package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/O-Marsters-1997/command-center/internal/store/ccdb"
)

type editTicketPayload struct {
	Branch    string   `json:"branch"`
	BlockedBy []string `json:"blocked_by"`
}

// QueueEditTicketIntent records one ticket's requested branch and blocked_by.
func (s *Store) QueueEditTicketIntent(ctx context.Context, e EditRequest) error {
	payload, err := json.Marshal(editTicketPayload{Branch: e.Branch, BlockedBy: nonNil(e.BlockedBy)})
	if err != nil {
		return fmt.Errorf("encode edit ticket intent payload for %s: %w", e.Ticket, err)
	}
	err = s.q.QueueEditTicketIntent(ctx, ccdb.QueueEditTicketIntentParams{
		At:       e.At.UTC(),
		TicketID: e.Ticket,
		Payload:  payload,
	})
	if err != nil {
		return fmt.Errorf("queue edit ticket intent for %s: %w", e.Ticket, err)
	}
	return nil
}

type EditTicketIntent struct {
	ID        int64
	TicketID  string
	Branch    string
	BlockedBy []string
}

// PendingEditTicketIntents returns every unconsumed ticket edit intent, oldest first.
func (s *Store) PendingEditTicketIntents(ctx context.Context) ([]EditTicketIntent, error) {
	rows, err := s.q.PendingEditTicketIntents(ctx)
	if err != nil {
		return nil, fmt.Errorf("select edit ticket intents: %w", err)
	}

	intents := make([]EditTicketIntent, 0, len(rows))
	for _, row := range rows {
		var payload editTicketPayload
		if err := json.Unmarshal(row.Payload, &payload); err != nil {
			return nil, fmt.Errorf("decode edit ticket intent payload for %s: %w", row.TicketID, err)
		}
		intents = append(intents, EditTicketIntent{
			ID: row.ID, TicketID: row.TicketID, Branch: payload.Branch, BlockedBy: payload.BlockedBy,
		})
	}
	return intents, nil
}

// EditTicket writes branch and blocked_by directly. A blocked_by breaking its own
// feature's closure is refused whole.
func (s *Store) EditTicket(ctx context.Context, ticketURL, branch string, blockedBy []string) error {
	return s.inTx(ctx, func(q *ccdb.Queries) error {
		feature, err := q.GetTicketFeature(ctx, ticketURL)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("read feature for %s: %w", ticketURL, err)
		}
		if feature != "" {
			intended, err := featureTicketsWithOverride(ctx, q, feature, blockedByOverride{URL: ticketURL, BlockedBy: blockedBy})
			if err != nil {
				return err
			}
			obs, _, err := lastObservation(ctx, q)
			if err != nil {
				return fmt.Errorf("read observation for closure check: %w", err)
			}
			featureOf, isMerged, err := closureLookups(ctx, q, intended, obs)
			if err != nil {
				return err
			}
			violation, err := closeUnderBlockedBy(feature, intended, featureOf, isMerged)
			if err != nil {
				return err
			}
			if violation != nil {
				return violation
			}
		}

		blob, err := encodeBlockedBy(blockedBy)
		if err != nil {
			return fmt.Errorf("encode blocked_by for %s: %w", ticketURL, err)
		}
		n, err := q.EditTicket(ctx, ccdb.EditTicketParams{Branch: branch, BlockedBy: blob, URL: ticketURL})
		return matchedRow(n, err, "edit ticket "+ticketURL)
	})
}
