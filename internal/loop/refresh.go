package loop

import (
	"context"
	"errors"
	"fmt"

	"github.com/O-Marsters-1997/command-center/internal/adapter/git"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

func (l *Loop) applyRefreshIntents(ctx context.Context, snap plan.Snapshot, obs plan.Observation) error {
	ctx = withSnapshot(ctx, snap)
	ticketIntents, orphanIntents := partitionRefreshIntents(snap)
	for _, r := range snap.Refreshes() {
		e, _ := snap.Entry(r.Ticket)
		result, err := l.refreshOne(ctx, snap, obs, e.Ticket, r.Row, r.Requested)
		if err != nil {
			return fmt.Errorf("%s/%s: %w", e.Ticket.Repo, e.Ticket.Branch, err)
		}
		if r.Requested {
			if err := l.consumeAll(ctx, ticketIntents[r.Ticket], result); err != nil {
				return err
			}
		}
	}
	return l.consumeAll(ctx, orphanIntents, failedWith(untrackedTicket))
}

func (l *Loop) consumeAll(ctx context.Context, intents []plan.Intent, result handled) error {
	return l.consumeEach(ctx, intents, func(plan.Intent) (handled, error) { return result, nil })
}

func partitionRefreshIntents(snap plan.Snapshot) (byTicket map[plan.TicketURL][]plan.Intent, orphans []plan.Intent) {
	byTicket = map[plan.TicketURL][]plan.Intent{}
	for _, i := range snap.Intents(plan.VerbRefresh) {
		if _, ok := snap.Entry(i.Ticket); ok {
			byTicket[i.Ticket] = append(byTicket[i.Ticket], i)
		} else {
			orphans = append(orphans, i)
		}
	}
	return byTicket, orphans
}

func conflictDetail(branchTip, baseTip plan.SHA, mergeErr error) string {
	return fmt.Sprintf("%s %s %s", branchTip, baseTip, mergeErr)
}

func (l *Loop) refreshOne(
	ctx context.Context, snap plan.Snapshot, obs plan.Observation, ticket plan.Ticket, row plan.PushRow, requested bool,
) (handled, error) {
	url := string(ticket.URL)
	branch := ticket.Branch
	refuse := func(detail string) (handled, error) {
		if !requested || snap.Conflict(ticket.URL).IsSome() {
			return failedWith(detail), nil
		}
		return l.refuse(ctx, url, store.EventRefreshRefused, detail)
	}

	entry, _ := snap.Entry(ticket.URL)
	if entry.HoldsWorktree() {
		return refuse(plan.HeldWorktreeRefusal)
	}
	worktreePath, refusal := idleWorktreeFor(ticket, obs)
	if refusal != "" {
		return refuse(refusal)
	}
	if obs.MidMerge[ticket.Ref()] {
		return failedWith("a merge is already in progress"), nil
	}

	if err := git.MergeFFOnly(ctx, worktreePath, "origin/"+branch); err != nil {
		return l.refuse(ctx, url, store.EventRefreshRefused, err.Error())
	}

	unlock := entry.Unlock
	if !unlock.Unlocked {
		return refuse(string(unlock.Reason))
	}
	restacked, detail, err := advanceOnto(ctx, worktreePath, ticket.Repo, unlock.BaseBranch, row, obs)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return handled{}, fmt.Errorf("advance %s: %w", url, ctxErr)
		}
		conflicted, midErr := git.MidMerge(ctx, worktreePath)
		if midErr != nil {
			return handled{}, fmt.Errorf("check %s for a conflict: %w", worktreePath, errors.Join(err, midErr))
		}
		if !conflicted {
			return handled{}, fmt.Errorf("advance %s: %w", url, err)
		}
		if restacked {
			if err := l.event(ctx, url, store.EventRestacked, detail+", conflicted"); err != nil {
				return handled{}, fmt.Errorf("record restack of %s: %w", url, err)
			}
		}
		baseTip := obs.BranchTips[plan.BranchRef{Repo: ticket.Repo, Branch: unlock.BaseBranch}]
		return failedWith("merge conflict with origin/" + unlock.BaseBranch), l.event(ctx, url, store.EventRefreshConflicted,
			conflictDetail(obs.BranchTips[ticket.Ref()], baseTip, err))
	}

	kind := store.EventRefreshed
	if restacked {
		kind = store.EventRestacked
	}
	if err := l.event(ctx, url, kind, fmt.Sprintf("merged origin/%s then %s", branch, detail)); err != nil {
		return handled{}, fmt.Errorf("record %s of %s: %w", kind, url, err)
	}
	return succeeded, nil
}

func advanceOnto(
	ctx context.Context, worktreePath, repo, base string, row plan.PushRow, obs plan.Observation,
) (bool, string, error) {
	ref := "origin/" + base
	boundary := string(restackBoundary(repo, row, obs))
	if boundary == "" {
		return false, ref, wrapGit("merge "+ref, git.Merge(ctx, worktreePath, ref))
	}
	kept, err := git.Ancestor(ctx, worktreePath, boundary, ref)
	if err != nil {
		return false, "", fmt.Errorf("check %s is an ancestor of %s: %w", boundary, ref, err)
	}
	if kept {
		return false, ref, wrapGit("merge "+ref, git.Merge(ctx, worktreePath, ref))
	}
	return true, fmt.Sprintf("restacked onto %s, dropping everything up to %s", ref, boundary),
		wrapGit("rebase onto "+ref, git.Rebase(ctx, worktreePath, ref, boundary))
}

func wrapGit(step string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", step, err)
}

func restackBoundary(repo string, row plan.PushRow, obs plan.Observation) plan.SHA {
	if row.BaseBranch == "" {
		return ""
	}
	if pr := obs.PRs[plan.BranchRef{Repo: repo, Branch: row.BaseBranch}]; pr.State == plan.Merged && pr.HeadOid != "" {
		return pr.HeadOid
	}
	return row.BaseSHAAtPush
}
