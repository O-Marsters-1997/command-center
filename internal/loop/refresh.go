package loop

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/command"
	"github.com/O-Marsters-1997/command-center/internal/git"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

func (l *Loop) applyRefreshIntents(ctx context.Context, snap plan.Snapshot, obs plan.Observation) error {
	for _, r := range snap.Refreshes() {
		e, _ := snap.Entry(r.Ticket)
		if err := l.refreshOne(ctx, snap, obs, e.Ticket, r.Row, r.Requested); err != nil {
			return fmt.Errorf("%s/%s: %w", e.Ticket.Repo, e.Ticket.Branch, err)
		}
		if r.Requested {
			if err := l.consumeAll(ctx, ticketIntents(snap, r.Ticket)); err != nil {
				return err
			}
		}
	}
	return l.consumeAll(ctx, orphanIntents(snap))
}

func (l *Loop) consumeAll(ctx context.Context, intents []plan.Intent) error {
	return l.consumeEach(ctx, intents, func(plan.Intent) error { return nil })
}

func ticketIntents(snap plan.Snapshot, url plan.TicketURL) []plan.Intent {
	return slices.DeleteFunc(snap.Intents(plan.VerbRefresh), func(i plan.Intent) bool { return i.Ticket != url })
}

func orphanIntents(snap plan.Snapshot) []plan.Intent {
	return slices.DeleteFunc(snap.Intents(plan.VerbRefresh), func(i plan.Intent) bool {
		_, ok := snap.Entry(i.Ticket)
		return ok
	})
}

func conflictDetail(branchTip, baseTip plan.SHA, mergeErr error) string {
	return fmt.Sprintf("%s %s %s", branchTip, baseTip, mergeErr)
}

func (l *Loop) refreshOne(
	ctx context.Context, snap plan.Snapshot, obs plan.Observation, ticket plan.Ticket, row plan.PushRow, requested bool,
) error {
	url := string(ticket.URL)
	branch := ticket.Branch
	refuse := func(detail string) error {
		if !requested {
			return nil
		}
		return l.event(ctx, url, store.EventRefreshRefused, detail)
	}

	entry, _ := snap.Entry(ticket.URL)
	if entry.HoldsWorktree() {
		return refuse(heldWorktreeRefusal)
	}
	worktreePath, refusal := idleWorktreeFor(ticket, obs)
	if refusal != "" {
		return refuse(refusal)
	}
	if obs.MidMerge[ticket.Ref()] {
		return refuse(fmt.Sprintf("%s is left mid-merge; abort or commit it first", worktreePath))
	}

	if err := git.MergeFFOnly(ctx, worktreePath, "origin/"+branch); err != nil {
		return l.event(ctx, url, store.EventRefreshRefused, err.Error())
	}

	unlock := entry.Unlock
	if !unlock.Unlocked {
		return refuse(string(unlock.Reason))
	}
	restacked, detail, err := advanceOnto(ctx, worktreePath, ticket.Repo, unlock.BaseBranch, row, obs)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("advance %s: %w", url, ctxErr)
		}
		conflicted, midErr := git.MidMerge(ctx, worktreePath)
		if midErr != nil {
			return fmt.Errorf("check %s for a conflict: %w", worktreePath, errors.Join(err, midErr))
		}
		if !conflicted {
			return fmt.Errorf("advance %s: %w", url, err)
		}
		if restacked {
			if err := l.event(ctx, url, store.EventRestacked, detail+", conflicted"); err != nil {
				return fmt.Errorf("record restack of %s: %w", url, err)
			}
		}
		baseTip := obs.BranchTips[plan.BranchRef{Repo: ticket.Repo, Branch: unlock.BaseBranch}]
		return l.event(ctx, url, store.EventRefreshConflicted,
			conflictDetail(obs.BranchTips[ticket.Ref()], baseTip, err))
	}

	kind := store.EventRefreshed
	if restacked {
		kind = store.EventRestacked
	}
	if err := l.event(ctx, url, kind, fmt.Sprintf("merged origin/%s then %s", branch, detail)); err != nil {
		return fmt.Errorf("record %s of %s: %w", kind, url, err)
	}
	if err := l.verifyOne(ctx, ticket, worktreePath, obs.Settings[ticket.Repo].VerifyCommand); err != nil {
		return fmt.Errorf("verify %s: %w", url, err)
	}
	return nil
}

const (
	maxVerifyDetail = 4000
	verifyTimeout   = 10 * time.Minute
)

func (l *Loop) verifyOne(ctx context.Context, ticket plan.Ticket, worktreePath string, argv []string) error {
	if len(argv) == 0 {
		return nil
	}
	out, err := command.OutputWithin(ctx, verifyTimeout, worktreePath, argv[0], argv[1:]...)
	if err == nil {
		return nil
	}
	detail := strings.TrimSpace(err.Error() + "\n" + string(out))
	if len(detail) > maxVerifyDetail {
		detail = detail[:maxVerifyDetail] + " …(truncated)"
	}
	detail = strings.ToValidUTF8(detail, "")
	return l.event(ctx, string(ticket.URL), store.EventVerificationFailed, detail)
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
