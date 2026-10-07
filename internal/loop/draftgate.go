package loop

import (
	"context"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

const (
	eventDraftReady       = "draft_ready"
	eventDraftReadyFailed = "draft_ready_failed"
)

func (l *Loop) applyDraftGate(ctx context.Context, snap plan.Snapshot) error {
	repoPaths := repoPathsByName(l.cfg.Repos)
	now := l.clock.Now()
	for _, e := range snap.Entries {
		if !e.ReadyToUndraft {
			continue
		}
		if err := l.readyOne(ctx, e.Ticket, repoPaths[e.Ticket.Repo], now); err != nil {
			return err
		}
	}
	return nil
}

func (l *Loop) readyOne(ctx context.Context, t plan.Ticket, repoPath string, now time.Time) error {
	event := store.Event{At: now, TicketURL: t.URL, Kind: eventDraftReady}
	if err := l.forge.Ready(ctx, repoPath, t.Branch); err != nil {
		event = store.Event{At: now, TicketURL: t.URL, Kind: eventDraftReadyFailed, Detail: err.Error()}
	}
	return l.store.AppendEvent(ctx, event)
}
