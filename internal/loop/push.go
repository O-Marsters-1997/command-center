package loop

import (
	"context"
	"fmt"
	"strings"

	"github.com/O-Marsters-1997/command-center/internal/git"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

const eventCommitResolutionRefused = "commit_resolution_refused"

func (l *Loop) pushPushable(ctx context.Context, snap plan.Snapshot, obs plan.Observation) error {
	lastPushed, err := l.store.LastPushedTips(ctx)
	if err != nil {
		return err
	}

	var candidates []plan.PushCandidate
	for _, e := range snap.Entries {
		if e.Run == nil || !e.Run.HasOutcome || e.Run.Outcome != plan.OutcomePush {
			continue
		}
		tip, ok := obs.LocalTips[plan.BranchKey(e.Ticket.Repo, e.Ticket.Branch)]
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
		return err
	}
	refreshFacts, err := l.store.RefreshFacts(ctx)
	if err != nil {
		return err
	}

	for _, ticketURL := range toPush {
		if facts[ticketURL].Failed || facts[ticketURL].Refused || refreshFacts[ticketURL].VerificationFailed {
			continue
		}
		e, _ := snap.Entry(ticketURL)
		tip := obs.LocalTips[plan.BranchKey(e.Ticket.Repo, e.Ticket.Branch)]
		if err := l.pushOne(ctx, e, tip, obs); err != nil {
			return err
		}
	}
	return nil
}

func (l *Loop) applyRetryPushIntents(ctx context.Context, snap plan.Snapshot, obs plan.Observation) error {
	return l.eachIntent(ctx, plan.VerbRetryPush, func(intent store.VerbIntent) error {
		e, ok := snap.Entry(intent.TicketID)
		if !ok {
			return nil
		}
		tip, ok := obs.LocalTips[plan.BranchKey(e.Ticket.Repo, e.Ticket.Branch)]
		if !ok {
			return nil
		}
		return l.pushOne(ctx, e, tip, obs)
	})
}

func (l *Loop) applyCommitResolutionIntents(ctx context.Context, snap plan.Snapshot, obs plan.Observation) error {
	return l.eachIntent(ctx, plan.VerbCommitResolution, func(intent store.VerbIntent) error {
		e, ok := snap.Entry(intent.TicketID)
		if !ok {
			return nil
		}
		return l.commitResolutionOne(ctx, e, obs)
	})
}

func (l *Loop) commitResolutionOne(ctx context.Context, e plan.Entry, obs plan.Observation) error {
	ticket := e.Ticket
	refuse := func(detail string) error {
		return l.event(ctx, ticket.URL, eventCommitResolutionRefused, detail)
	}

	worktreePath, refusal := idleWorktreeFor(ticket, obs)
	if refusal != "" {
		return refuse(refusal)
	}

	unmerged, err := git.UnmergedPaths(ctx, worktreePath)
	if err != nil {
		return fmt.Errorf("read unmerged paths for %s: %w", ticket.URL, err)
	}
	if len(unmerged) > 0 {
		return refuse(fmt.Sprintf("still unmerged: %s", strings.Join(unmerged, ", ")))
	}

	midMerge, err := git.MidMerge(ctx, worktreePath)
	if err != nil {
		return fmt.Errorf("read merge state for %s: %w", ticket.URL, err)
	}
	if midMerge {
		staged, err := git.StagedPaths(ctx, worktreePath)
		if err != nil {
			return fmt.Errorf("read staged paths for %s: %w", ticket.URL, err)
		}
		if len(staged) == 0 {
			return refuse("nothing staged to commit")
		}
		if err := git.CommitNoEdit(ctx, worktreePath); err != nil {
			return refuse(err.Error())
		}
	}

	tip, err := git.BranchTip(ctx, l.repo(ticket.Repo).Checkout, ticket.Branch)
	if err != nil {
		return fmt.Errorf("read tip after commit resolution for %s: %w", ticket.URL, err)
	}
	return l.pushOne(ctx, e, tip, obs)
}

func pushBranch(ctx context.Context, repoPath, branch, recordedTip string, restacked bool) error {
	if !restacked || recordedTip == "" {
		return git.Push(ctx, repoPath, branch)
	}
	descended, err := git.Ancestor(ctx, repoPath, recordedTip, branch)
	if err != nil {
		return err
	}
	if descended {
		return git.Push(ctx, repoPath, branch)
	}
	return git.PushRestacked(ctx, repoPath, branch, recordedTip)
}

func (l *Loop) pushOne(ctx context.Context, e plan.Entry, localTip string, obs plan.Observation) error {
	t := e.Ticket
	if !e.Unlock.Unlocked {
		return nil
	}
	base := e.Unlock.BaseBranch
	repoPath := l.repo(t.Repo).Checkout

	changed, err := git.ChangedPaths(ctx, repoPath, "origin/"+base, t.Branch)
	if err != nil {
		return fmt.Errorf("diff %s against origin/%s: %w", t.Branch, base, err)
	}
	if refused, path := plan.PushRefused(changed, plan.Policy{Deny: obs.Settings[t.Repo].Deny}); refused {
		return l.event(ctx, t.URL, store.EventPushRefused, path)
	}

	pushedTips, err := l.store.LastPushedTips(ctx)
	if err != nil {
		return err
	}
	restacked, err := l.store.RestackedSinceLastPush(ctx)
	if err != nil {
		return err
	}
	if err := pushBranch(ctx, repoPath, t.Branch, pushedTips[t.URL], restacked[t.URL]); err != nil {
		return l.event(ctx, t.URL, store.EventPushFailed, err.Error())
	}

	if obs.PRs[plan.BranchKey(t.Repo, t.Branch)].State != plan.Open {
		body := plan.PRBody(base, obs.PRs[plan.BranchKey(t.Repo, base)].Number)
		worktree := obs.Worktrees[plan.BranchKey(t.Repo, t.Branch)]
		if err := l.forge.Create(ctx, worktree, base, body, e.OpensAsDraft); err != nil {
			return l.event(ctx, t.URL, store.EventPushFailed, err.Error())
		}
	}

	baseSHA, err := git.RevParse(ctx, repoPath, "origin/"+base)
	if err != nil {
		return fmt.Errorf("resolve origin/%s: %w", base, err)
	}
	if err := l.store.RecordPush(ctx, t.URL, localTip, base, baseSHA, l.clock.Now()); err != nil {
		return err
	}
	return l.event(ctx, t.URL, store.EventPushed, fmt.Sprintf("pushed %s to origin/%s", localTip, base))
}
