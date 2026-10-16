package loop

import (
	"context"
	"fmt"

	"github.com/O-Marsters-1997/command-center/internal/git"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

func (l *Loop) pushPushable(ctx context.Context, snap plan.Snapshot, obs plan.Observation) error {
	for _, p := range snap.Pushable(obs) {
		e, _ := snap.Entry(p.Ticket)
		if err := l.pushOne(ctx, e, p, obs); err != nil {
			return fmt.Errorf("%s/%s: %w", e.Ticket.Repo, e.Ticket.Branch, err)
		}
	}
	return nil
}

func (l *Loop) applyRetryPushIntents(ctx context.Context, snap plan.Snapshot, obs plan.Observation) error {
	return l.eachIntent(ctx, snap, plan.VerbRetryPush, func(intent plan.Intent) error {
		e, ok := snap.Entry(intent.Ticket)
		if !ok {
			return nil
		}
		p, ok := snap.Push(intent.Ticket, obs)
		if !ok {
			return nil
		}
		if err := l.pushOne(ctx, e, p, obs); err != nil {
			return fmt.Errorf("%s/%s: %w", e.Ticket.Repo, e.Ticket.Branch, err)
		}
		return nil
	})
}

func pushBranch(ctx context.Context, repoPath, branch string, tip plan.SHA, restacked bool) error {
	recordedTip := string(tip)
	if !restacked || recordedTip == "" {
		return wrapGit("push "+branch, git.Push(ctx, repoPath, branch))
	}
	descended, err := git.Ancestor(ctx, repoPath, recordedTip, branch)
	if err != nil {
		return fmt.Errorf("check %s descends from %s: %w", branch, recordedTip, err)
	}
	if descended {
		return wrapGit("push "+branch, git.Push(ctx, repoPath, branch))
	}
	return wrapGit("force-push restacked "+branch, git.PushRestacked(ctx, repoPath, branch, recordedTip))
}

func (l *Loop) pushOne(ctx context.Context, e plan.Entry, p plan.Push, obs plan.Observation) error {
	t := e.Ticket
	url := string(t.URL)
	base := e.Unlock.BaseBranch
	repoPath := l.checkout(t.Repo)

	changed, err := git.ChangedPaths(ctx, repoPath, "origin/"+base, t.Branch)
	if err != nil {
		return fmt.Errorf("diff %s against origin/%s: %w", t.Branch, base, err)
	}
	if refused, path := plan.PushRefused(changed, plan.Policy{Deny: obs.Settings[t.Repo].Deny}); refused {
		return l.event(ctx, url, store.EventPushRefused, path)
	}

	var lastPushed plan.SHA
	if e.LastPush.IsSome() {
		lastPushed = e.LastPush.Unwrap().PushedTip
	}
	if err := pushBranch(ctx, repoPath, t.Branch, lastPushed, p.Force); err != nil {
		return l.event(ctx, url, store.EventPushFailed, err.Error())
	}

	if obs.PRs[t.Ref()].State != plan.Open {
		worktree := obs.Worktrees[t.Ref()]
		if err := l.forge.Create(ctx, worktree, base, p.Body, p.OpensAsDraft); err != nil {
			return l.event(ctx, url, store.EventPushFailed, err.Error())
		}
	}

	baseSHA, err := git.RevParse(ctx, repoPath, "origin/"+base)
	if err != nil {
		return fmt.Errorf("resolve origin/%s: %w", base, err)
	}
	push := store.Push{
		Ticket: plan.TicketURL(url), Tip: p.LocalTip, BaseSHA: plan.SHA(baseSHA), BaseBranch: base, At: l.clock.Now(),
	}
	if err := l.store.RecordPush(ctx, push); err != nil {
		return fmt.Errorf("record push of %s: %w", t.Branch, err)
	}
	return l.event(ctx, url, store.EventPushed, fmt.Sprintf("pushed %s to origin/%s", p.LocalTip, base))
}
