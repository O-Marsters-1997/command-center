package cc

import "github.com/O-Marsters-1997/command-center/internal/plan"

// conflictedBase names the base a launch would cut this ticket from when that base already carries
// a merge conflict, and "" when it is clean. Unclean means either that the base does not merge
// into main, or that its own worktree is stopped on an unresolved merge. A child cut from such a
// base inherits the conflict and re-fights it on every restack, so no launch is ever made from
// one (docs/adr/0006-resolve-a-conflict-once.md).
//
// A locked row is judged on the base it would get once unlocked, which is what makes an
// authorised row say why it is waiting instead of stalling at the next tick's cut.
func conflictedBase(
	t plan.Ticket, byURL map[string]plan.Ticket, unlock plan.Unlock, stacking bool, obs Observation,
) string {
	base := unlock.BaseBranch
	if base == "" {
		base = plan.ProspectiveBase(t, byURL, stacking)
	}
	if base == defaultBaseBranch {
		return ""
	}
	if obs.ConflictsWithBase[base] || obs.MidMerge[base] {
		return base
	}
	return ""
}
