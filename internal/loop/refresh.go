package loop

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/git"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

// retry-push bypasses PushFacts, then sweeps the eligible base-moved rows
// (docs/designs/command-centre-design.md § 4a).
func (l *Loop) applyRefreshIntents(ctx context.Context, snap plan.Snapshot, obs plan.Observation) error {
	now := l.clock.Now()

	intents, err := l.store.PendingVerbIntents(ctx, plan.VerbRefresh)
	if err != nil {
		return err
	}
	requested := make(map[string]bool, len(intents))
	for _, intent := range intents {
		requested[intent.TicketID] = true
		if e, ok := snap.Entry(intent.TicketID); ok {
			var row plan.PushRow
			if e.LastPush != nil {
				row = *e.LastPush
			}
			if err := l.refreshOne(ctx, snap, obs, e.Ticket, row, now, true); err != nil {
				return err
			}
		}
		if err := l.store.ConsumeVerbIntent(ctx, intent.ID, now); err != nil {
			return err
		}
	}

	return l.autoRefresh(ctx, snap, obs, requested, now)
}

// autoRefresh sweeps every pushed, base-moved row that no live run or unresolved merge bars
// (inv. 4) and whose last refresh-domain attempt neither refused, conflicted nor failed
// verification, so a human's abort is not undone by the next tick re-running the same merge
// (docs/designs/command-centre-design.md § 4a).
func (l *Loop) autoRefresh(
	ctx context.Context, snap plan.Snapshot, obs plan.Observation, requested map[string]bool, now time.Time,
) error {
	outcomes, err := l.store.LatestRefreshOutcomes(ctx)
	if err != nil {
		return err
	}

	for _, e := range snap.Entries {
		t := e.Ticket
		if requested[t.URL] {
			continue
		}
		if e.Run == nil || !e.Run.HasOutcome || e.Run.Outcome != plan.OutcomePush {
			continue
		}
		if obs.PRs[branchKey(t.Repo, t.Branch)].State != plan.Open {
			continue
		}
		pushRow := e.LastPush
		if pushRow == nil || !baseMoved(*pushRow, obs, t.Repo) {
			continue
		}
		if o, tried := outcomes[t.URL]; tried && !supersededConflict(o, t, *pushRow, obs) {
			continue
		}
		if err := l.refreshOne(ctx, snap, obs, t, *pushRow, now, false); err != nil {
			return err
		}
	}
	return nil
}

// conflictDetail packs the two tips a failed refresh's merge step attempted into
// store.EventRefreshConflicted's Detail: the branch's own origin tip it had just fast-forwarded to, and
// the base's origin tip it tried to merge in. parseConflictDetail reads them back.
func conflictDetail(branchTip, baseTip string, mergeErr error) string {
	return fmt.Sprintf("%s %s %s", branchTip, baseTip, mergeErr)
}

func parseConflictDetail(detail string) (branchTip, baseTip string, ok bool) {
	fields := strings.SplitN(detail, " ", 3)
	if len(fields) < 3 {
		return "", "", false
	}
	return fields[0], fields[1], true
}

// supersededConflict reports whether a stale refresh_conflicted outcome still describes the merge
// autoRefresh is about to retry. Either tip moving since the failed attempt -- the branch's own,
// pushed by a human outside the app, or the base's, advanced again with a later fix -- makes it a
// different merge from the one that failed, so the gate no longer applies to it (issue #188). Any
// other refresh-domain outcome keeps gating until the refresh verb clears it.
func supersededConflict(o store.RefreshOutcome, t plan.Ticket, row plan.PushRow, obs plan.Observation) bool {
	if o.Kind != store.EventRefreshConflicted {
		return false
	}
	branchTip, baseTip, ok := parseConflictDetail(o.Detail)
	if !ok {
		return false
	}
	return obs.BranchTips[branchKey(t.Repo, t.Branch)] != branchTip ||
		obs.BranchTips[branchKey(t.Repo, row.BaseBranch)] != baseTip
}

// baseMoved is the git-level fact §4a marks a row on: the row's recorded base -- a stacked
// branch, or main once retargetMerged has pointed it there -- whose current tip differs from
// what was recorded at the ticket's last push (issue #85: main counts the same as a stacked base).
func baseMoved(row plan.PushRow, obs plan.Observation, repo string) bool {
	return row.BaseBranch != "" && obs.BranchTips[branchKey(repo, row.BaseBranch)] != row.BaseSHAAtPush
}

// refreshOne fast-forwards one ticket's own branch, advances it onto its base, then verifies the
// result. A refused fast-forward records refresh_refused and stops; a conflict is left mid-merge
// for a human (docs/designs/command-centre-design.md § 4a).
func (l *Loop) refreshOne(
	ctx context.Context, snap plan.Snapshot, obs plan.Observation, ticket plan.Ticket, row plan.PushRow,
	now time.Time, requested bool,
) error {
	branch := ticket.Branch
	refuse := func(detail string) error {
		if !requested {
			return nil
		}
		return l.store.AppendEvent(ctx,
			store.Event{At: now, TicketURL: ticket.URL, Kind: store.EventRefreshRefused, Detail: detail})
	}

	worktreePath, ok := obs.Worktrees[branchKey(ticket.Repo, branch)]
	switch {
	case !ok:
		return refuse(fmt.Sprintf("no worktree for %s", branch))
	case obs.Runs[ticket.URL].Alive:
		return refuse(fmt.Sprintf("a run is alive in %s", worktreePath))
	case obs.MidMerge[branchKey(ticket.Repo, branch)]:
		return refuse(fmt.Sprintf("%s is left mid-merge; abort or commit it first", worktreePath))
	}

	if err := git.MergeFFOnly(ctx, worktreePath, "origin/"+branch); err != nil {
		return l.store.AppendEvent(ctx, store.Event{
			At: now, TicketURL: ticket.URL, Kind: store.EventRefreshRefused, Detail: err.Error(),
		})
	}

	entry, _ := snap.Entry(ticket.URL)
	unlock := entry.Unlock
	if !unlock.Unlocked {
		return refuse(string(unlock.Reason))
	}
	restacked, detail, err := advanceOnto(ctx, worktreePath, ticket.Repo, unlock.BaseBranch, row, obs)
	if err != nil {
		// A rebase that stops on a conflict has already rewritten the branch, so the push after
		// whoever resolves it still needs the lease the completed restack would have earned
		// (issue #93). A conflicted merge rewrites nothing and earns nothing.
		if restacked {
			if err := l.store.AppendEvent(ctx, store.Event{
				At: now, TicketURL: ticket.URL, Kind: store.EventRestacked,
				Detail: detail + ", conflicted",
			}); err != nil {
				return err
			}
		}
		baseTip := obs.BranchTips[branchKey(ticket.Repo, unlock.BaseBranch)]
		return l.store.AppendEvent(ctx, store.Event{
			At: now, TicketURL: ticket.URL, Kind: store.EventRefreshConflicted,
			Detail: conflictDetail(obs.BranchTips[branchKey(ticket.Repo, branch)], baseTip, err),
		})
	}

	kind := store.EventRefreshed
	if restacked {
		kind = store.EventRestacked
	}
	if err := l.store.AppendEvent(ctx, store.Event{
		At: now, TicketURL: ticket.URL, Kind: kind,
		Detail: fmt.Sprintf("merged origin/%s then %s", branch, detail),
	}); err != nil {
		return err
	}
	return l.verifyOne(ctx, ticket, worktreePath, verifyCommandByRepo(l.cfg.Repos)[ticket.Repo], now)
}

const maxVerifyDetail = 4000

// ponytail: runs synchronously in the tick, like refresh's own git calls -- if a slow build ever
// measurably stalls the 15s loop, move it onto the async Runner spawnRun already uses.
func (l *Loop) verifyOne(
	ctx context.Context, ticket plan.Ticket, worktreePath string, argv []string, now time.Time,
) error {
	if len(argv) == 0 {
		return nil
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = worktreePath
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	detail := strings.TrimSpace(string(out))
	if len(detail) > maxVerifyDetail {
		detail = detail[:maxVerifyDetail] + " …(truncated)"
	}
	return l.store.AppendEvent(ctx, store.Event{
		At: now, TicketURL: ticket.URL, Kind: store.EventVerificationFailed,
		Detail: fmt.Sprintf("%s: %s: %s", strings.Join(argv, " "), err, detail),
	})
}

// advanceOnto merges the base when the base only moved forward, and restacks when the base's
// history no longer contains what the branch was built on -- a squash-merged parent, or a base
// the app itself rewrote a tick earlier (issue #89). Merging in that case replays the branch's
// own copies of commits the base already carries under new SHAs, which conflicts on every line
// either side touched. It reports which of the two it did, because only a restack licenses the
// push step to lease-force, and what the event should say.
func advanceOnto(
	ctx context.Context, worktreePath, repo, base string, row plan.PushRow, obs plan.Observation,
) (bool, string, error) {
	ref := "origin/" + base
	boundary := restackBoundary(repo, row, obs)
	if boundary == "" {
		return false, ref, git.Merge(ctx, worktreePath, ref)
	}
	kept, err := git.Ancestor(ctx, worktreePath, boundary, ref)
	if err != nil {
		return false, "", err
	}
	if kept {
		return false, ref, git.Merge(ctx, worktreePath, ref)
	}
	return true, fmt.Sprintf("restacked onto %s, dropping everything up to %s", ref, boundary),
		git.Rebase(ctx, worktreePath, ref, boundary)
}

// restackBoundary is the last commit of the branch's recorded base that its own commits sit on
// top of, so a restack drops exactly the work the base already carries. A merged base is read
// from its pull request's head, not from base_sha_at_push, because a base that advanced after
// this branch's last push has those later commits in the squash too (issue #89).
func restackBoundary(repo string, row plan.PushRow, obs plan.Observation) string {
	if row.BaseBranch == "" {
		return ""
	}
	if pr := obs.PRs[branchKey(repo, row.BaseBranch)]; pr.State == plan.Merged && pr.HeadOid != "" {
		return pr.HeadOid
	}
	return row.BaseSHAAtPush
}
