package loop

import (
	"context"
	"fmt"

	"github.com/O-Marsters-1997/command-center/internal/git"
	"github.com/O-Marsters-1997/command-center/internal/option"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

func (l *Loop) pushPushable(ctx context.Context, snap plan.Snapshot, obs plan.Observation) error {
	lastPushed, err := l.store.LastPushedTips(ctx)
	if err != nil {
		return fmt.Errorf("last pushed tips: %w", err)
	}

	var candidates []plan.PushCandidate
	for _, e := range snap.Entries {
		if e.Run.IsNone() || e.Run.Unwrap().Outcome != option.Some(plan.OutcomePush) {
			continue
		}
		tip, ok := obs.LocalTips[e.Ticket.Ref()]
		if !ok || obs.Runs[e.Ticket.URL].Alive {
			continue
		}
		candidates = append(candidates,
			plan.PushCandidate{URL: e.Ticket.URL, LocalTip: tip, LastPushedTip: lastPushed[e.Ticket.URL]})
	}

	toPush := plan.PushPlan(candidates)
	if len(toPush) == 0 {
		return nil
	}

	facts, err := l.store.PushFacts(ctx)
	if err != nil {
		return fmt.Errorf("push facts: %w", err)
	}
	refreshFacts, err := l.store.RefreshFacts(ctx)
	if err != nil {
		return fmt.Errorf("refresh facts: %w", err)
	}

	for _, ticketURL := range toPush {
		if facts[ticketURL].Failed || facts[ticketURL].Refused || refreshFacts[ticketURL].VerificationFailed {
			continue
		}
		e, _ := snap.Entry(ticketURL)
		tip := obs.LocalTips[e.Ticket.Ref()]
		if err := l.pushOne(ctx, e, tip, obs); err != nil {
			return fmt.Errorf("%s/%s: %w", e.Ticket.Repo, e.Ticket.Branch, err)
		}
	}
	return nil
}

func (l *Loop) applyRetryPushIntents(ctx context.Context, snap plan.Snapshot, obs plan.Observation) error {
	return l.eachIntent(ctx, plan.VerbRetryPush, func(intent store.VerbIntent) error {
		e, ok := snap.Entry(plan.TicketURL(intent.TicketID))
		if !ok {
			return nil
		}
		tip, ok := obs.LocalTips[e.Ticket.Ref()]
		if !ok {
			return nil
		}
		if err := l.pushOne(ctx, e, tip, obs); err != nil {
			return fmt.Errorf("%s/%s: %w", e.Ticket.Repo, e.Ticket.Branch, err)
		}
		return nil
	})
}

type pushTarget struct {
	repoPath, branch string
	recordedTip      plan.SHA
	restacked        bool
}

func pushBranch(ctx context.Context, target pushTarget) error {
	repoPath, branch, recordedTip := target.repoPath, target.branch, string(target.recordedTip)
	if !target.restacked || recordedTip == "" {
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

func (l *Loop) pushOne(ctx context.Context, e plan.Entry, localTip plan.SHA, obs plan.Observation) error {
	t := e.Ticket
	url := string(t.URL)
	if !e.Unlock.Unlocked {
		return nil
	}
	base := e.Unlock.BaseBranch
	repoPath := l.checkout(t.Repo)

	changed, err := git.ChangedPaths(ctx, repoPath, "origin/"+base, t.Branch)
	if err != nil {
		return fmt.Errorf("diff %s against origin/%s: %w", t.Branch, base, err)
	}
	if refused, path := plan.PushRefused(changed, plan.Policy{Deny: obs.Settings[t.Repo].Deny}); refused {
		return l.event(ctx, url, store.EventPushRefused, path)
	}

	pushedTips, err := l.store.LastPushedTips(ctx)
	if err != nil {
		return fmt.Errorf("last pushed tips: %w", err)
	}
	restacked, err := l.store.RestackedSinceLastPush(ctx)
	if err != nil {
		return fmt.Errorf("restacked since last push: %w", err)
	}
	target := pushTarget{
		repoPath: repoPath, branch: t.Branch, recordedTip: pushedTips[t.URL], restacked: restacked[t.URL],
	}
	if err := pushBranch(ctx, target); err != nil {
		return l.event(ctx, url, store.EventPushFailed, err.Error())
	}

	if obs.PRs[t.Ref()].State != plan.Open {
		body := plan.PRBody(base, obs.PRs[plan.BranchRef{Repo: t.Repo, Branch: base}].Number)
		worktree := obs.Worktrees[t.Ref()]
		if err := l.forge.Create(ctx, worktree, base, body, e.OpensAsDraft); err != nil {
			return l.event(ctx, url, store.EventPushFailed, err.Error())
		}
	}

	baseSHA, err := git.RevParse(ctx, repoPath, "origin/"+base)
	if err != nil {
		return fmt.Errorf("resolve origin/%s: %w", base, err)
	}
	if err := l.store.RecordPush(ctx, url, string(localTip), base, baseSHA, l.clock.Now()); err != nil {
		return fmt.Errorf("record push of %s: %w", t.Branch, err)
	}
	return l.event(ctx, url, store.EventPushed, fmt.Sprintf("pushed %s to origin/%s", localTip, base))
}
