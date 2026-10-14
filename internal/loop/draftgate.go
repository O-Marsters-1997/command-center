package loop

import (
	"context"
	"fmt"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

func (l *Loop) applyDraftGate(ctx context.Context, snap plan.Snapshot) error {
	for _, e := range snap.Entries {
		if !e.ReadyToUndraft {
			continue
		}
		if err := l.readyOne(ctx, e.Ticket); err != nil {
			return fmt.Errorf("%s/%s: %w", e.Ticket.Repo, e.Ticket.Branch, err)
		}
	}
	return nil
}

func (l *Loop) readyOne(ctx context.Context, t plan.Ticket) error {
	if err := l.forge.Ready(ctx, l.checkout(t.Repo), t.Branch); err != nil {
		return l.event(ctx, string(t.URL), store.EventDraftReadyFailed, err.Error())
	}
	return l.event(ctx, string(t.URL), store.EventDraftReady, "")
}
