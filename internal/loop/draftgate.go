package loop

import (
	"context"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

const (
	eventDraftReady       = "draft_ready"
	eventDraftReadyFailed = "draft_ready_failed"
)

func (l *Loop) applyDraftGate(ctx context.Context, snap plan.Snapshot) error {
	for _, e := range snap.Entries {
		if !e.ReadyToUndraft {
			continue
		}
		if err := l.readyOne(ctx, e.Ticket); err != nil {
			return err
		}
	}
	return nil
}

func (l *Loop) readyOne(ctx context.Context, t plan.Ticket) error {
	if err := l.forge.Ready(ctx, l.repo(t.Repo).Checkout, t.Branch); err != nil {
		return l.event(ctx, t.URL, eventDraftReadyFailed, err.Error())
	}
	return l.event(ctx, t.URL, eventDraftReady, "")
}
