package cc

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

// applyDraftGate un-drafts every open PR plan.DraftGate says is ready, and never re-drafts. A
// failed `gh pr ready` is never latched -- the next tick simply retries against a fresh
// observation (docs/designs/command-centre-design.md § 6 job 2, inv. 13).
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

// readyOne calls `gh pr ready` for one ticket, recording either outcome as an event: a failure is
// never latched, so leaving the row untouched here is exactly what lets the next tick retry.
func (l *Loop) readyOne(ctx context.Context, t plan.Ticket, repoPath string, now time.Time) error {
	event := store.Event{At: now, TicketURL: t.URL, Kind: eventDraftReady}
	if err := l.forge.Ready(ctx, repoPath, t.Branch); err != nil {
		event = store.Event{At: now, TicketURL: t.URL, Kind: eventDraftReadyFailed, Detail: err.Error()}
	}
	return l.store.AppendEvent(ctx, event)
}
