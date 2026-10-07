package loop

import (
	"context"
	"fmt"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

const (
	eventRetargeted     = "retargeted"
	eventRetargetFailed = "retarget_failed"
)

// Mergify's queue takes main-based pull requests only, and both repos delete a merged branch.
func (l *Loop) retargetMerged(ctx context.Context, snap plan.Snapshot, obs plan.Observation) error {
	now := l.clock.Now()
	for _, e := range snap.Entries {
		t, row := e.Ticket, e.LastPush
		if row == nil || row.BaseBranch == "" || row.BaseBranch == defaultBaseBranch {
			continue
		}
		if obs.PRs[branchKey(t.Repo, t.Branch)].State != plan.Open ||
			obs.PRs[branchKey(t.Repo, row.BaseBranch)].State != plan.Merged {
			continue
		}
		if err := l.retargetOne(ctx, snap, obs, t, *row, now); err != nil {
			return err
		}
	}
	return nil
}

func (l *Loop) retargetOne(
	ctx context.Context, snap plan.Snapshot, obs plan.Observation, t plan.Ticket, row plan.PushRow, now time.Time,
) error {
	repoPath := repoPathsByName(l.cfg.Repos)[t.Repo]
	if err := l.forge.Edit(ctx, repoPath, t.Branch, defaultBaseBranch); err != nil {
		return l.store.AppendEvent(ctx, store.Event{
			At: now, TicketURL: t.URL, Kind: eventRetargetFailed, Detail: err.Error(),
		})
	}

	if err := l.store.RecordPush(ctx, t.URL, row.PushedTip, defaultBaseBranch, row.BaseSHAAtPush, now); err != nil {
		return err
	}
	if err := l.store.AppendEvent(ctx, store.Event{
		At: now, TicketURL: t.URL, Kind: eventRetargeted,
		Detail: fmt.Sprintf("re-pointed %s from %s at %s, which merged", t.Branch, row.BaseBranch, defaultBaseBranch),
	}); err != nil {
		return err
	}
	return l.refreshOne(ctx, snap, obs, t, row, now, false)
}
