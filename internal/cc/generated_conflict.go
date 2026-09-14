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
		if !obs.ConflictsWithBase[t.Branch] || obs.MidMerge[t.Branch] || obs.Runs[t.URL].Alive {
			continue
		}
		worktreePath, ok := obs.Worktrees[t.Branch]
		if !ok {
			continue
		}
		policy := plan.GeneratedPolicy{Paths: generated[t.Repo], BuildCommand: buildCommand[t.Repo]}
		paths := obs.ConflictedPaths[t.Branch]
		if !plan.AllGenerated(paths, policy) {
			continue
		}
		if err := l.regenerateAndCommit(ctx, t, worktreePath, paths, policy.BuildCommand, now); err != nil {
			return err
		}
	}
	return nil
}

// regenerateAndCommit merges origin/main -- which plan.AllGenerated has already established
// conflicts only on paths -- reruns the repo's build command to regenerate them, and completes
// the merge over the result. gitSucceeds's exit-1 case is that same conflict, not a failure; any
// other error is a real one and is returned rather than silently built and committed over.
func (l *Loop) regenerateAndCommit(
	ctx context.Context, t Ticket, worktreePath string, paths, buildCommand []string, now time.Time,
) error {
	if _, err := gitSucceeds(ctx, worktreePath, "merge", "origin/"+defaultBaseBranch); err != nil {
		return fmt.Errorf("merge origin/%s into %s for %s: %w", defaultBaseBranch, t.Branch, t.URL, err)
	}

	cmd := exec.CommandContext(ctx, buildCommand[0], buildCommand[1:]...)
	cmd.Dir = worktreePath
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("run build command %q for %s: %w: %s", strings.Join(buildCommand, " "), t.URL, err, out)
	}

	if err := Add(ctx, worktreePath, paths); err != nil {
		return err
	}
	if err := Commit(ctx, worktreePath, "Regenerate after merging origin/"+defaultBaseBranch); err != nil {
		return err
	}
	return l.store.AppendEvent(ctx, Event{
		At: now, TicketURL: t.URL, Kind: eventGeneratedConflictResolved,
		Detail: fmt.Sprintf("merged origin/%s, ran %s, committed", defaultBaseBranch, strings.Join(buildCommand, " ")),
	})
}
