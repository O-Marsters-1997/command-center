package loop

import (
	"context"
	"fmt"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

const (
	eventRetargeted     = "retargeted"
	eventRetargetFailed = "retarget_failed"
)

// Mergify's queue takes main-based pull requests only, and both repos delete a merged branch.
func (l *Loop) retargetMerged(ctx context.Context, snap plan.Snapshot, obs plan.Observation) error {
	for _, e := range snap.Entries {
		t, row := e.Ticket, e.LastPush
		if row == nil || row.BaseBranch == "" || row.BaseBranch == plan.DefaultBaseBranch {
			continue
		}
		if obs.PRs[plan.BranchKey(t.Repo, t.Branch)].State != plan.Open ||
			obs.PRs[plan.BranchKey(t.Repo, row.BaseBranch)].State != plan.Merged {
			continue
		}
		if err := l.retargetOne(ctx, snap, obs, t, *row); err != nil {
			return err
		}
	}
	return nil
}

func (l *Loop) retargetOne(
	ctx context.Context, snap plan.Snapshot, obs plan.Observation, t plan.Ticket, row plan.PushRow,
) error {
	repoPath := l.checkout(t.Repo)
	if err := l.forge.Edit(ctx, repoPath, t.Branch, plan.DefaultBaseBranch); err != nil {
		return l.event(ctx, t.URL, eventRetargetFailed, err.Error())
	}

	err := l.store.RecordPush(ctx, t.URL, row.PushedTip, plan.DefaultBaseBranch, row.BaseSHAAtPush, l.clock.Now())
	if err != nil {
		return err
	}
	detail := fmt.Sprintf("re-pointed %s from %s at %s, which merged", t.Branch, row.BaseBranch, plan.DefaultBaseBranch)
	if err := l.event(ctx, t.URL, eventRetargeted, detail); err != nil {
		return err
	}
	return l.refreshOne(ctx, snap, obs, t, row, false)
}
