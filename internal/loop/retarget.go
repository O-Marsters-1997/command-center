package loop

import (
	"context"
	"fmt"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

// Mergify's queue takes main-based pull requests only, and both repos delete a merged branch.
func (l *Loop) retargetMerged(ctx context.Context, snap plan.Snapshot, obs plan.Observation) error {
	for _, e := range snap.Entries {
		t := e.Ticket
		if e.HoldsWorktree() || e.LastPush.IsNone() {
			continue
		}
		row := e.LastPush.Unwrap()
		if row.BaseBranch == "" || row.BaseBranch == plan.DefaultBaseBranch {
			continue
		}
		if obs.PRs[t.Ref()].State != plan.Open ||
			obs.PRs[plan.BranchRef{Repo: t.Repo, Branch: row.BaseBranch}].State != plan.Merged {
			continue
		}
		if err := l.retargetOne(ctx, snap, obs, refreshJob{ticket: t, row: row}); err != nil {
			return fmt.Errorf("%s/%s: %w", t.Repo, t.Branch, err)
		}
	}
	return nil
}

func (l *Loop) retargetOne(
	ctx context.Context, snap plan.Snapshot, obs plan.Observation, job refreshJob,
) error {
	t, row := job.ticket, job.row
	url := string(t.URL)
	repoPath := l.checkout(t.Repo)
	if err := l.forge.Edit(ctx, repoPath, t.Branch, plan.DefaultBaseBranch); err != nil {
		return l.event(ctx, url, store.EventRetargetFailed, err.Error())
	}

	err := l.store.RecordPush(ctx, store.Push{
		Ticket: t.URL, Tip: row.PushedTip, BaseSHA: row.BaseSHAAtPush, BaseBranch: plan.DefaultBaseBranch,
		At: l.clock.Now(),
	})
	if err != nil {
		return fmt.Errorf("record retargeted push: %w", err)
	}
	detail := fmt.Sprintf("re-pointed %s from %s at %s, which merged", t.Branch, row.BaseBranch, plan.DefaultBaseBranch)
	if err := l.event(ctx, url, store.EventRetargeted, detail); err != nil {
		return fmt.Errorf("record retarget: %w", err)
	}
	if err := l.refreshOne(ctx, snap, obs, job); err != nil {
		return fmt.Errorf("refresh after retarget: %w", err)
	}
	return nil
}
