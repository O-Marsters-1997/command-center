package cc

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

// retargetMerged re-points every descendant whose parent has merged at the default branch.
// Both repos delete a merged branch, and Mergify's queue takes main-based pull requests only
// (docs/designs/command-centre-design.md § 4a).
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

// retargetOne records nothing when gh refuses, so the next tick retries: a retarget is
// idempotent, unlike the push whose failure waits for a human's retry-push verb.
// It closes with the same step the refresh performs, because base_sha_at_push would otherwise
// record a main this branch's content was never tried against (issue #85). The row it hands on
// is the pre-retarget one: naming the merged parent is what tells advanceOnto to restack rather
// than merge a squash that shares no ancestry with this branch (issue #89).
func (l *Loop) retargetOne(
	ctx context.Context, snap plan.Snapshot, obs plan.Observation, t plan.Ticket, row plan.PushRow, now time.Time,
) error {
	repoPath := repoPathsByName(l.cfg.Repos)[t.Repo]
	if err := l.forge.Edit(ctx, repoPath, t.Branch, defaultBaseBranch); err != nil {
		return l.store.AppendEvent(ctx, store.Event{
			At: now, TicketURL: t.URL, Kind: eventRetargetFailed, Detail: err.Error(),
		})
	}

	// The base branch has to change, or this retarget re-runs `gh pr edit` every tick. The base
	// SHA must not: recording main's tip here claims the branch already sits on main, and the
	// refresh below declines whenever the worktree is mid-merge or a run is live, which leaves
	// baseMoved comparing main against itself and the row never advancing again (issue #95).
	// Carrying the merged parent's tip through is also what restackBoundary needs, since a
	// main-based row has no base pull request to read a head from.
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
