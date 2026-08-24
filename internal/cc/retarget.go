package cc

import (
	"context"
	"fmt"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/gh"
)

const eventRetargeted = "retargeted"

// retargetMerged re-points every descendant whose parent has merged at the default branch.
// Both repos delete a merged branch, and Mergify's queue takes main-based pull requests only
// (docs/designs/command-centre-design.md § 4a).
func (l *Loop) retargetMerged(ctx context.Context, obs Observation) error {
	tasks, err := l.store.Tasks(ctx)
	if err != nil {
		return err
	}
	pushRows, err := l.store.LatestPushes(ctx)
	if err != nil {
		return err
	}

	repoPaths := repoPathsByName(l.ws.Root, l.cfg.Repos)
	now := l.now()
	for _, t := range tasks {
		row, pushed := pushRows[t.TicketURL]
		if !pushed || row.BaseBranch == "" || row.BaseBranch == defaultBaseBranch {
			continue
		}
		if obs.PRs[t.Branch].State != gh.Open || obs.PRs[row.BaseBranch].State != gh.Merged {
			continue
		}
		if err := l.retargetOne(ctx, t, row, repoPaths[t.Repo], now); err != nil {
			return err
		}
	}
	return nil
}

// retargetOne re-points one descendant's pull request and records the new base against the same
// pushed_tip. gh.Edit is called whatever base GitHub reports, since GitHub's own
// delete-branch-on-merge retarget racing ours is the normal case, not an error.
func (l *Loop) retargetOne(ctx context.Context, t Task, row PushRow, repoPath string, now time.Time) error {
	if err := gh.Edit(ctx, repoPath, t.Branch, defaultBaseBranch); err != nil {
		return fmt.Errorf("retarget %s at %s: %w", t.Branch, defaultBaseBranch, err)
	}

	baseSHA, err := RevParse(ctx, repoPath, "origin/"+defaultBaseBranch)
	if err != nil {
		return fmt.Errorf("resolve origin/%s: %w", defaultBaseBranch, err)
	}
	if err := l.store.RecordPush(ctx, t.TicketURL, row.PushedTip, defaultBaseBranch, baseSHA, now); err != nil {
		return err
	}
	return l.store.AppendEvent(ctx, Event{
		At: now, TaskURL: t.TicketURL, Kind: eventRetargeted,
		Detail: fmt.Sprintf("re-pointed %s from %s at %s, which merged", t.Branch, row.BaseBranch, defaultBaseBranch),
	})
}
