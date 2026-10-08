package loop

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/O-Marsters-1997/command-center/internal/git"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

func (l *Loop) applyRefreshIntents(ctx context.Context, snap plan.Snapshot, obs plan.Observation) error {
	requested := map[string]bool{}
	err := l.eachIntent(ctx, plan.VerbRefresh, func(intent store.VerbIntent) error {
		requested[intent.TicketID] = true
		e, ok := snap.Entry(intent.TicketID)
		if !ok {
			return nil
		}
		var row plan.PushRow
		if e.LastPush != nil {
			row = *e.LastPush
		}
		return l.refreshOne(ctx, snap, obs, e.Ticket, row, true)
	})
	if err != nil {
		return err
	}
	return l.autoRefresh(ctx, snap, obs, requested)
}

func (l *Loop) autoRefresh(
	ctx context.Context, snap plan.Snapshot, obs plan.Observation, requested map[string]bool,
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
		if obs.PRs[plan.BranchKey(t.Repo, t.Branch)].State != plan.Open {
			continue
		}
		pushRow := e.LastPush
		if pushRow == nil || !baseMoved(*pushRow, obs, t.Repo) {
			continue
		}
		if o, tried := outcomes[t.URL]; tried && !supersededConflict(o, t, *pushRow, obs) {
			continue
		}
		if err := l.refreshOne(ctx, snap, obs, t, *pushRow, false); err != nil {
			return err
		}
	}
	return nil
}

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

func supersededConflict(o store.RefreshOutcome, t plan.Ticket, row plan.PushRow, obs plan.Observation) bool {
	if o.Kind != store.EventRefreshConflicted {
		return false
	}
	branchTip, baseTip, ok := parseConflictDetail(o.Detail)
	if !ok {
		return false
	}
	return obs.BranchTips[plan.BranchKey(t.Repo, t.Branch)] != branchTip ||
		obs.BranchTips[plan.BranchKey(t.Repo, row.BaseBranch)] != baseTip
}

func baseMoved(row plan.PushRow, obs plan.Observation, repo string) bool {
	return row.BaseBranch != "" && obs.BranchTips[plan.BranchKey(repo, row.BaseBranch)] != row.BaseSHAAtPush
}

func (l *Loop) refreshOne(
	ctx context.Context, snap plan.Snapshot, obs plan.Observation, ticket plan.Ticket, row plan.PushRow,
	requested bool,
) error {
	branch := ticket.Branch
	refuse := func(detail string) error {
		if !requested {
			return nil
		}
		return l.event(ctx, ticket.URL, store.EventRefreshRefused, detail)
	}

	worktreePath, refusal := idleWorktreeFor(ticket, obs)
	if refusal != "" {
		return refuse(refusal)
	}
	if obs.MidMerge[plan.BranchKey(ticket.Repo, branch)] {
		return refuse(fmt.Sprintf("%s is left mid-merge; abort or commit it first", worktreePath))
	}

	if err := git.MergeFFOnly(ctx, worktreePath, "origin/"+branch); err != nil {
		return l.event(ctx, ticket.URL, store.EventRefreshRefused, err.Error())
	}

	entry, _ := snap.Entry(ticket.URL)
	unlock := entry.Unlock
	if !unlock.Unlocked {
		return refuse(string(unlock.Reason))
	}
	restacked, detail, err := advanceOnto(ctx, worktreePath, ticket.Repo, unlock.BaseBranch, row, obs)
	if err != nil {
		if restacked {
			if err := l.event(ctx, ticket.URL, store.EventRestacked, detail+", conflicted"); err != nil {
				return err
			}
		}
		baseTip := obs.BranchTips[plan.BranchKey(ticket.Repo, unlock.BaseBranch)]
		return l.event(ctx, ticket.URL, store.EventRefreshConflicted,
			conflictDetail(obs.BranchTips[plan.BranchKey(ticket.Repo, branch)], baseTip, err))
	}

	kind := store.EventRefreshed
	if restacked {
		kind = store.EventRestacked
	}
	if err := l.event(ctx, ticket.URL, kind, fmt.Sprintf("merged origin/%s then %s", branch, detail)); err != nil {
		return err
	}
	return l.verifyOne(ctx, ticket, worktreePath, l.repo(ticket.Repo).VerifyCommand)
}

const maxVerifyDetail = 4000

func (l *Loop) verifyOne(ctx context.Context, ticket plan.Ticket, worktreePath string, argv []string) error {
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
	return l.event(ctx, ticket.URL, store.EventVerificationFailed,
		fmt.Sprintf("%s: %s: %s", strings.Join(argv, " "), err, detail))
}

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

func restackBoundary(repo string, row plan.PushRow, obs plan.Observation) string {
	if row.BaseBranch == "" {
		return ""
	}
	if pr := obs.PRs[plan.BranchKey(repo, row.BaseBranch)]; pr.State == plan.Merged && pr.HeadOid != "" {
		return pr.HeadOid
	}
	return row.BaseSHAAtPush
}
