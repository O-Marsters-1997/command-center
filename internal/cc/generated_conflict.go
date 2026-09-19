package cc

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

const eventGeneratedConflictResolved = "generated_conflict_resolved"

// resolveGeneratedConflicts merges origin/main into every previously-pushed branch whose conflict
// with it is confined to paths this repo's build regenerates, rebuilds them, and commits the
// result -- the one conflict shape that needs no judgement and so gets no agent, no run and no
// verb (issue #177). pushPushable delivers the commit; this writes none of its own (push.go:54).
func (l *Loop) resolveGeneratedConflicts(ctx context.Context, obs Observation) error {
	tickets, err := l.store.Tickets(ctx)
	if err != nil {
		return err
	}
	pushed, err := l.store.LatestPushes(ctx)
	if err != nil {
		return err
	}
	generated := generatedByRepo(l.cfg.Repos)
	buildCommand := buildCommandByRepo(l.cfg.Repos)
	now := l.now()

	for _, t := range tickets {
		if _, ok := pushed[t.URL]; !ok {
			continue // nothing under review yet for pushPushable to deliver this to
		}
		key := branchKey(t.Repo, t.Branch)
		if !obs.ConflictsWithBase[key] || obs.MidMerge[key] || obs.Runs[t.URL].Alive {
			continue
		}
		worktreePath, ok := obs.Worktrees[key]
		if !ok {
			continue
		}
		policy := plan.GeneratedPolicy{Paths: generated[t.Repo], BuildCommand: buildCommand[t.Repo]}
		if !plan.AllGenerated(obs.ConflictedPaths[key], policy) {
			continue
		}
		if err := l.regenerateAndCommit(ctx, t, worktreePath, policy, now); err != nil {
			return err
		}
	}
	return nil
}

func (l *Loop) regenerateAndCommit(
	ctx context.Context, t Ticket, worktreePath string, policy plan.GeneratedPolicy, now time.Time,
) error {
	if err := MergeFFOnly(ctx, worktreePath, "origin/"+t.Branch); err != nil {
		return nil
	}

	// gitSucceeds's exit-1 case is git merge's own conflict exit status, not a failure.
	if _, err := gitSucceeds(ctx, worktreePath, "merge", "origin/"+defaultBaseBranch); err != nil {
		return fmt.Errorf("merge origin/%s into %s for %s: %w", defaultBaseBranch, t.Branch, t.URL, err)
	}

	unmerged, err := UnmergedPaths(ctx, worktreePath)
	if err != nil {
		return err
	}
	if !plan.AllGenerated(unmerged, policy) {
		return MergeAbort(ctx, worktreePath)
	}

	cmd := exec.CommandContext(ctx, policy.BuildCommand[0], policy.BuildCommand[1:]...)
	cmd.Dir = worktreePath
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("run build command %q for %s: %w: %s", strings.Join(policy.BuildCommand, " "), t.URL, err, out)
	}

	if err := Add(ctx, worktreePath, unmerged); err != nil {
		return err
	}
	if err := Commit(ctx, worktreePath, "Regenerate after merging origin/"+defaultBaseBranch); err != nil {
		return err
	}
	return l.store.AppendEvent(ctx, Event{
		At: now, TicketURL: t.URL, Kind: eventGeneratedConflictResolved,
		Detail: fmt.Sprintf("merged origin/%s, ran %s, committed", defaultBaseBranch, strings.Join(policy.BuildCommand, " ")),
	})
}
