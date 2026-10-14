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
		t, row := e.Ticket, e.LastPush
		if e.HoldsWorktree() {
			continue
		}
		if row == nil || row.BaseBranch == "" || row.BaseBranch == plan.DefaultBaseBranch {
			continue
		}
		if obs.PRs[plan.BranchKey(t.Repo, t.Branch)].State != plan.Open ||
			obs.PRs[plan.BranchKey(t.Repo, row.BaseBranch)].State != plan.Merged {
			continue
		}
		if err := l.retargetOne(ctx, snap, obs, t, *row); err != nil {
			return fmt.Errorf("%s/%s: %w", t.Repo, t.Branch, err)
		}
	}
	return nil
}

func (l *Loop) retargetOne(
	ctx context.Context, snap plan.Snapshot, obs plan.Observation, t plan.Ticket, row plan.PushRow,
) error {
	repoPath := l.checkout(t.Repo)
	if err := l.forge.Edit(ctx, repoPath, t.Branch, plan.DefaultBaseBranch); err != nil {
		return l.event(ctx, t.URL, store.EventRetargetFailed, err.Error())
	}

	err := l.store.RecordPush(ctx, t.URL, row.PushedTip, plan.DefaultBaseBranch, row.BaseSHAAtPush, l.clock.Now())
	if err != nil {
		return fmt.Errorf("record retargeted push: %w", err)
	}
	detail := fmt.Sprintf("re-pointed %s from %s at %s, which merged", t.Branch, row.BaseBranch, plan.DefaultBaseBranch)
	if err := l.event(ctx, t.URL, store.EventRetargeted, detail); err != nil {
		return fmt.Errorf("record retarget: %w", err)
	}
	if err := l.refreshOne(ctx, snap, obs, t, row, false); err != nil {
		return fmt.Errorf("refresh after retarget: %w", err)
	}
	return nil
}
