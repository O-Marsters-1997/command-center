package loop

import (
	"context"
	"errors"
	"fmt"

	"github.com/O-Marsters-1997/command-center/internal/adapter/gh"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

func (l *Loop) applyAutoFollowUps(
	ctx context.Context, snap plan.Snapshot, t *tick, byURL map[plan.TicketURL]store.Ticket,
) error {
	for _, auto := range snap.AutoFollowUps() {
		ticket, ok := byURL[auto.Ticket]
		entry, found := snap.Entry(auto.Ticket)
		if !ok || !found {
			continue
		}
		if _, refusal := idleWorktreeFor(ticket.Plan(), t.obs); refusal != "" {
			continue
		}
		picked := store.FollowUpPayload{Message: l.autoMessage(auto, entry, t.obs.PRs[ticket.Ref()].Checks)}
		err := l.followUpOne(ctx, snap, t, ticket, picked, auto.Cause)
		if err != nil && !errors.Is(err, errIntentPending) {
			return fmt.Errorf("%s/%s: %w", ticket.Repo, ticket.Branch, err)
		}
	}
	return nil
}

func (l *Loop) autoMessage(auto plan.AutoFollowUp, entry plan.Entry, checks map[string]plan.CheckState) string {
	if auto.Cause == plan.CauseVerifyFailed {
		return plan.ComposeFixVerify(l.tailOf(auto.LogPath))
	}
	return plan.ComposeFixCI(entry.RedChecks(checks))
}

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
