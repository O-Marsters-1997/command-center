package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/O-Marsters-1997/command-center/internal/plan"
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
		Payload:  notNull(string(payload)),
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
		if err := json.Unmarshal([]byte(row.Payload.String), &payload); err != nil {
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
func (s *Store) EditTicket(ctx context.Context, ticketURL, branch string, blockedBy []string) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, tx.Rollback())
		}
	}()

	qtx := s.q.WithTx(tx)
	feature, err := qtx.TicketFeature(ctx, ticketURL)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("read feature for %s: %w", ticketURL, err)
	}
	if feature != "" {
		var intended []ticketBlockedBy
		override := blockedByOverride{URL: ticketURL, BlockedBy: blockedBy}
		intended, err = featureTicketsWithOverride(ctx, qtx, feature, override)
		if err != nil {
			return fmt.Errorf("read feature tickets for %s: %w", ticketURL, err)
		}
		var obs plan.Observation
		obs, _, err = s.LastObservation(ctx)
		if err != nil {
			return fmt.Errorf("read observation for closure check: %w", err)
		}
		var violation *FeatureClosureError
		violation, err = closeUnderBlockedBy(
			feature, intended, ticketFeatureLookup(ctx, qtx), blockerMergedLookup(ctx, qtx, obs),
		)
		if err != nil {
			return fmt.Errorf("check feature closure for %s: %w", ticketURL, err)
		}
		if violation != nil {
			return violation
		}
	}

	var blob []byte
	blob, err = encodeBlockedBy(blockedBy)
	if err != nil {
		return fmt.Errorf("encode blocked_by for %s: %w", ticketURL, err)
	}
	if err = qtx.EditTicket(ctx, ccdb.EditTicketParams{Branch: branch, BlockedBy: blob, URL: ticketURL}); err != nil {
		return fmt.Errorf("edit ticket %s: %w", ticketURL, err)
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit edit of ticket %s: %w", ticketURL, err)
	}
	return nil
}
