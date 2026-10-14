package loop

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/command"
	"github.com/O-Marsters-1997/command-center/internal/git"
	"github.com/O-Marsters-1997/command-center/internal/option"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

func (l *Loop) applyRefreshIntents(ctx context.Context, snap plan.Snapshot, obs plan.Observation) error {
	requested := map[plan.TicketURL]bool{}
	err := l.eachIntent(ctx, plan.VerbRefresh, func(intent store.VerbIntent) error {
		url := plan.TicketURL(intent.TicketID)
		requested[url] = true
		e, ok := snap.Entry(url)
		if !ok {
			return nil
		}
		row := e.LastPush.UnwrapOr(plan.PushRow{})
		if err := l.refreshOne(ctx, snap, obs, refreshJob{ticket: e.Ticket, row: row, requested: true}); err != nil {
			return fmt.Errorf("%s/%s: %w", e.Ticket.Repo, e.Ticket.Branch, err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return l.autoRefresh(ctx, snap, obs, requested)
}

func (l *Loop) autoRefresh(
	ctx context.Context, snap plan.Snapshot, obs plan.Observation, requested map[plan.TicketURL]bool,
) error {
	outcomes, err := l.store.LatestRefreshOutcomes(ctx)
	if err != nil {
		return fmt.Errorf("latest refresh outcomes: %w", err)
	}

	for _, e := range snap.Entries {
		t := e.Ticket
		if requested[t.URL] {
			continue
		}
		if e.Run.IsNone() || e.Run.Unwrap().Outcome != option.Some(plan.OutcomePush) {
			continue
		}
		if obs.PRs[t.Ref()].State != plan.Open {
			continue
		}
		if e.LastPush.IsNone() {
			continue
		}
		pushRow := e.LastPush.Unwrap()
		if !baseMoved(pushRow, obs, t.Repo) {
			continue
		}
		if o, tried := outcomes[t.URL]; tried && !supersededConflict(o, t, pushRow, obs) {
			continue
		}
		if err := l.refreshOne(ctx, snap, obs, refreshJob{ticket: t, row: pushRow}); err != nil {
			return fmt.Errorf("%s/%s: %w", t.Repo, t.Branch, err)
		}
	}
	return nil
}

func conflictDetail(branchTip, baseTip plan.SHA, mergeErr error) string {
	return fmt.Sprintf("%s %s %s", branchTip, baseTip, mergeErr)
}

func parseConflictDetail(detail string) (branchTip, baseTip plan.SHA, ok bool) {
	fields := strings.SplitN(detail, " ", 3)
	if len(fields) < 3 {
		return "", "", false
	}
	return plan.SHA(fields[0]), plan.SHA(fields[1]), true
}

func supersededConflict(o store.RefreshOutcome, t plan.Ticket, row plan.PushRow, obs plan.Observation) bool {
	if o.Kind != store.EventRefreshConflicted {
		return false
	}
	branchTip, baseTip, ok := parseConflictDetail(o.Detail)
	if !ok {
		return false
	}
	return obs.BranchTips[t.Ref()] != branchTip ||
		obs.BranchTips[plan.BranchRef{Repo: t.Repo, Branch: row.BaseBranch}] != baseTip
}

func baseMoved(row plan.PushRow, obs plan.Observation, repo string) bool {
	return row.BaseBranch != "" &&
		obs.BranchTips[plan.BranchRef{Repo: repo, Branch: row.BaseBranch}] != row.BaseSHAAtPush
}

type refreshJob struct {
	ticket    plan.Ticket
	row       plan.PushRow
	requested bool
}

func (l *Loop) refreshOne(ctx context.Context, snap plan.Snapshot, obs plan.Observation, job refreshJob) error {
	ticket, row, requested := job.ticket, job.row, job.requested
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
	adv := advance{worktreePath: worktreePath, repo: ticket.Repo, base: unlock.BaseBranch, row: row}
	restacked, detail, err := advanceOnto(ctx, adv, obs)
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
	return l.event(ctx, string(ticket.URL), store.EventVerificationFailed, detail)
}

type advance struct {
	worktreePath, repo, base string
	row                      plan.PushRow
}

func advanceOnto(ctx context.Context, adv advance, obs plan.Observation) (bool, string, error) {
	worktreePath := adv.worktreePath
	ref := "origin/" + adv.base
	boundary := string(restackBoundary(adv.repo, adv.row, obs))
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
