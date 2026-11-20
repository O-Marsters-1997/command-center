package loop

import (
	"context"
	"fmt"

	"github.com/O-Marsters-1997/command-center/internal/adapter/git"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

func (l *Loop) applyResolveIntents(
	ctx context.Context, snap plan.Snapshot, t *tick, byURL map[plan.TicketURL]store.Ticket,
) error {
	return l.eachIntent(ctx, snap, plan.VerbResolve, func(intent plan.Intent) error {
		ticket, ok := l.ticket(byURL, string(intent.Ticket))
		if !ok {
			return nil
		}
		if err := l.resolveOne(ctx, snap, t, ticket, byURL); err != nil {
			return fmt.Errorf("%s/%s: %w", ticket.Repo, ticket.Branch, err)
		}
		return nil
	})
}

func (l *Loop) resolveOne(
	ctx context.Context, snap plan.Snapshot, t *tick, ticket store.Ticket, byURL map[plan.TicketURL]store.Ticket,
) error {
	url := plan.TicketURL(ticket.URL)
	refuse := func(reason string) error { return l.event(ctx, ticket.URL, store.EventResolveRefused, reason) }

	if !snap.Offers(url, plan.VerbResolve) {
		return refuse("the ticket is not refresh conflicted")
	}
	conflict := snap.Conflict(url)
	if conflict.IsNone() {
		return refuse("no conflict is recorded for this ticket")
	}
	at := conflict.Unwrap()
	worktreePath, refusal := idleWorktreeFor(ticket.Plan(), t.obs)
	if refusal != "" {
		return refuse(refusal)
	}
	pair := fmt.Sprintf("%s %s", at.BranchTip, at.BaseTip)
	tried, err := l.store.HasEventDetail(ctx, ticket.URL, store.EventResolveStarted, pair)
	if err != nil {
		return fmt.Errorf("check earlier resolve of %s: %w", ticket.URL, err)
	}
	if tried {
		return refuse(fmt.Sprintf("a resolve already ran at %s", pair))
	}
	paths, err := git.ConflictedPaths(ctx, worktreePath)
	if err != nil {
		return fmt.Errorf("read conflicts in %s: %w", worktreePath, err)
	}
	if len(paths) == 0 {
		return refuse(fmt.Sprintf("no conflicted paths in %s", worktreePath))
	}

	entry, _ := snap.Entry(url)
	base := entry.Unlock.BaseBranch
	brief := plan.ResolveBrief{
		Title: ticket.Title, BaseTitle: base, BaseBranch: base, At: at, Paths: paths,
	}
	for _, peer := range byURL {
		if peer.Repo == ticket.Repo && peer.Branch == base {
			brief.BaseTitle = peer.Title
			break
		}
	}

	baseline, err := git.BranchTip(ctx, l.checkout(ticket.Repo), ticket.Branch)
	if err != nil {
		return fmt.Errorf("read baseline for resolve of %s: %w", ticket.URL, err)
	}
	if err := l.event(ctx, ticket.URL, store.EventResolveStarted, pair); err != nil {
		return err
	}
	if err := l.spawnRun(ctx, t, spawnSpec{
		ticket: ticket, worktree: worktreePath, baseline: baseline,
		kind: plan.RunKindResolve, prompt: plan.ComposeResolve(brief), choice: entry.Ticket.Choice,
	}); err != nil {
		return fmt.Errorf("spawn resolve in %s: %w", worktreePath, err)
	}
	return nil
}
