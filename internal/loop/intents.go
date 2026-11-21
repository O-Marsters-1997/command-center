package loop

import (
	"context"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

type handled struct {
	outcome plan.IntentOutcome
	detail  string
	run     option.Option[plan.RunID]
}

const untrackedTicket = "the ticket is not tracked"

var succeeded = handled{outcome: plan.IntentDone}

func failedWith(detail string) handled {
	return handled{outcome: plan.IntentFailed, detail: detail}
}

func spawned(id plan.RunID) handled {
	return handled{run: option.Some(id)}
}

func (l *Loop) refuse(ctx context.Context, url string, kind store.EventKind, detail string) (handled, error) {
	return failedWith(detail), l.event(ctx, url, kind, detail)
}

type snapshotKey struct{}

func withSnapshot(ctx context.Context, snap plan.Snapshot) context.Context {
	return context.WithValue(ctx, snapshotKey{}, snap)
}

func ticketState(ctx context.Context, url plan.TicketURL) plan.State {
	snap, ok := ctx.Value(snapshotKey{}).(plan.Snapshot)
	if !ok {
		return plan.StateUnknown
	}
	entry, ok := snap.Entry(url)
	if !ok {
		return plan.StateUnknown
	}
	return entry.State
}
