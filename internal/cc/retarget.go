package cc

import (
	"context"
	"fmt"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/gh"
)

const (
	eventRetargeted     = "retargeted"
	eventRetargetFailed = "retarget_failed"
)

// retargetMerged re-points every descendant whose parent has merged at the default branch.
// Both repos delete a merged branch, and Mergify's queue takes main-based pull requests only
// (docs/designs/command-centre-design.md § 4a).
func (l *Loop) retargetMerged(ctx context.Context, obs Observation) error {
	tasks, err := l.store.Tasks(ctx)
	if err != nil {
		return err
	}
	rc, err := l.newRefreshContext(ctx, tasks, obs)
	if err != nil {
		return err
	}

	now := l.now()
	for _, t := range tasks {
		row, pushed := rc.pushRows[t.TicketURL]
		if !pushed || row.BaseBranch == "" || row.BaseBranch == defaultBaseBranch {
			continue
		}
		if obs.PRs[t.Branch].State != gh.Open || obs.PRs[row.BaseBranch].State != gh.Merged {
			continue
		}
		if err := l.retargetOne(ctx, t, row, rc, now); err != nil {
			return err
		}
	}
	return nil
}

// retargetOne records nothing when gh refuses, so the next tick retries: a retarget is
// idempotent, unlike the push whose failure waits for a human's retry-push verb.
// It closes with the same step the refresh performs, because base_sha_at_push would otherwise
// record a main this branch's content was never tried against (issue #85). The row it hands on
// is the pre-retarget one: naming the merged parent is what tells advanceOnto to restack rather
// than merge a squash that shares no ancestry with this branch (issue #89).
func (l *Loop) retargetOne(ctx context.Context, t Task, row PushRow, rc refreshContext, now time.Time) error {
	repoPath := rc.repoPaths[t.Repo]
	if err := gh.Edit(ctx, repoPath, t.Branch, defaultBaseBranch); err != nil {
		return l.store.AppendEvent(ctx, Event{
			At: now, TaskURL: t.TicketURL, Kind: eventRetargetFailed, Detail: err.Error(),
		})
	}

	baseSHA, err := RevParse(ctx, repoPath, "origin/"+defaultBaseBranch)
	if err != nil {
		return fmt.Errorf("resolve origin/%s: %w", defaultBaseBranch, err)
	}
	if err := l.store.RecordPush(ctx, t.TicketURL, row.PushedTip, defaultBaseBranch, baseSHA, now); err != nil {
		return err
	}
	if err := l.store.AppendEvent(ctx, Event{
		At: now, TaskURL: t.TicketURL, Kind: eventRetargeted,
		Detail: fmt.Sprintf("re-pointed %s from %s at %s, which merged", t.Branch, row.BaseBranch, defaultBaseBranch),
	}); err != nil {
		return err
	}
	return l.refreshOne(ctx, t, row, rc, now)
}
