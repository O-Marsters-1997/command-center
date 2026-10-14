package view

import (
	"context"

	"github.com/O-Marsters-1997/command-center/internal/store"
)

const eventPageSize = 500

func EachTicketEvent(
	ctx context.Context, st *store.Store, ticketURL string, afterID int64, fn func(store.Event) error,
) error {
	for {
		page, err := st.TicketEventsAfter(ctx, ticketURL, afterID, eventPageSize)
		if err != nil {
			return err
		}
		for _, e := range page {
			if err := fn(e); err != nil {
				return err
			}
			afterID = e.ID
		}
		if len(page) < eventPageSize {
			return nil
		}
	}
}
