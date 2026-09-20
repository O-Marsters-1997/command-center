package cc

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc/ccdb"
)

// editTicketPayload is an intent's free-form payload for verb "edit_ticket" -- the frozen v1
// schema's intents.payload is JSON precisely so a new verb's own fields need no migration.
type editTicketPayload struct {
	Branch    string   `json:"branch"`
	BlockedBy []string `json:"blocked_by"`
}

// QueueEditTicketIntent records one ticket's requested branch and blocked_by. A handler only
// ever does this one blind INSERT; the loop is the sole reader and actor on it (inv. 9).
func (s *Store) QueueEditTicketIntent(
	ctx context.Context, ticketURL, branch string, blockedBy []string, at time.Time,
) error {
	payload, err := json.Marshal(editTicketPayload{Branch: branch, BlockedBy: nonNil(blockedBy)})
	if err != nil {
		return fmt.Errorf("encode edit ticket intent payload for %s: %w", ticketURL, err)
	}
	err = s.q.QueueEditTicketIntent(ctx, ccdb.QueueEditTicketIntentParams{
		At:       at.UTC(),
		TicketID: ticketURL,
		Payload:  notNull(string(payload)),
	})
	if err != nil {
		return fmt.Errorf("queue edit ticket intent for %s: %w", ticketURL, err)
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

// EditTicket writes branch and blocked_by directly -- the only two columns POST /ticket can
// change, refreshed only by ImportTickets otherwise. A blocked_by breaking its own feature's
// closure is refused whole rather than written.
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
		intended, err = featureTicketsWithOverride(ctx, qtx, feature, ticketURL, blockedBy)
		if err != nil {
			return err
		}
		var obs Observation
		obs, _, err = s.LastObservation(ctx)
		if err != nil {
			return fmt.Errorf("read observation for closure check: %w", err)
		}
		if violation := closeUnderBlockedBy(
			feature, intended, ticketFeatureLookup(ctx, qtx), blockerMergedLookup(ctx, qtx, obs),
		); violation != nil {
			return violation
		}
	}

	blob, _ := json.Marshal(nonNil(blockedBy)) // json.Marshal of a []string cannot error
	if err = qtx.EditTicket(ctx, ccdb.EditTicketParams{Branch: branch, BlockedBy: blob, URL: ticketURL}); err != nil {
		return fmt.Errorf("edit ticket %s: %w", ticketURL, err)
	}
	return tx.Commit()
}
