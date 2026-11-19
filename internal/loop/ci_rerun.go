package loop

import (
	"context"

	"github.com/O-Marsters-1997/command-center/internal/adapter/gh"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

func (l *Loop) rerunFailedChecks(ctx context.Context, snap plan.Snapshot, byURL map[plan.TicketURL]store.Ticket) {
	reran := map[string]bool{}
	for _, r := range snap.Reruns() {
		id, err := gh.RunID(r.DetailsURL)
		if err != nil || reran[id] {
			continue
		}
		reran[id] = true
		if err := l.forge.Rerun(ctx, l.checkout(byURL[r.Ticket].Repo), r.DetailsURL); err != nil {
			l.logger.Warn("rerun failed check", "ticket", r.Ticket, "err", err)
		}
	}
}
