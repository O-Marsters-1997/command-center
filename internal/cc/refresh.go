package cc

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/gh"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

// refreshVerb is the verb a base-moved row offers for the worktree its own live run or a
// refused fast-forward keeps the automatic pass from touching (docs/designs/command-centre-design.md § 4a).
const refreshVerb = plan.VerbRefresh

const (
	eventRefreshRefused = "refresh_refused"
	eventRefreshed      = "refreshed"
)

// RefreshFact is a task's outstanding refused fast-forward, mirroring PushFact exactly (push.go):
// derived from the latest refresh_refused event since the task's last recorded push, so a later
// successful refresh-and-push -- which the existing push step delivers the same tick -- clears it.
type RefreshFact struct {
	Refused bool
	Reason  string
}

// RefreshFacts returns every task's outstanding refused fast-forward, keyed by ticket URL: what
// gates the automatic refresh pass's retry (a refusal is never retried automatically -- the
// refresh verb is) and the page's needs-you rendering both read.
func (s *Store) RefreshFacts(ctx context.Context) (map[string]RefreshFact, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.task_id, e.detail
		FROM events e
		JOIN (
			SELECT e2.task_id, MAX(e2.id) AS id
			FROM events e2
			LEFT JOIN (
				SELECT task_id, MAX(pushed_at) AS pushed_at FROM pushes GROUP BY task_id
			) p ON p.task_id = e2.task_id
			WHERE e2.kind = ? AND e2.at > COALESCE(p.pushed_at, '')
			GROUP BY e2.task_id
		) latest ON latest.task_id = e.task_id AND latest.id = e.id`,
		eventRefreshRefused)
	if err != nil {
		return nil, fmt.Errorf("select refresh facts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	facts := map[string]RefreshFact{}
	for rows.Next() {
		var taskID string
		var detail sql.NullString
		if err := rows.Scan(&taskID, &detail); err != nil {
			return nil, fmt.Errorf("scan refresh fact: %w", err)
		}
		facts[taskID] = RefreshFact{Refused: true, Reason: detail.String}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate refresh facts: %w", err)
	}
	return facts, nil
}

// refreshContext is the per-tick facts refreshOne needs, gathered once by applyRefreshIntents
// rather than re-queried per task -- pushContext's own shape (push.go).
type refreshContext struct {
	byURL     map[string]plan.Task
	stacking  map[string]bool
	prs       map[string]plan.PRState
	repoPaths map[string]string
	obs       Observation
}

func (l *Loop) newRefreshContext(tasks []Task, obs Observation) refreshContext {
	return refreshContext{
		byURL:     planTasksByURL(tasks),
		stacking:  stackingByRepo(l.cfg.Repos),
		prs:       prsByBranch(obs),
		repoPaths: repoPathsByName(l.ws.Root, l.cfg.Repos),
		obs:       obs,
	}
}

// applyRefreshIntents consumes every pending refresh request -- a human's explicit retry, which
// runs the contract regardless of RefreshFacts' gate exactly as retry-push bypasses PushFacts'
// (push.go) -- then sweeps every base-moved row with no live run, no unresolved merge and no
// outstanding refusal: the automatic half of §4a (plans/command-centre-phase-2.md § Phase 5).
func (l *Loop) applyRefreshIntents(ctx context.Context, obs Observation) error {
	tasks, err := l.store.Tasks(ctx)
	if err != nil {
		return err
	}
	byTicket := tasksByTicket(tasks)
	rc := l.newRefreshContext(tasks, obs)
	now := l.now()

	intents, err := l.store.PendingVerbIntents(ctx, refreshVerb)
	if err != nil {
		return err
	}
	requested := make(map[string]bool, len(intents))
	for _, intent := range intents {
		requested[intent.TaskID] = true
		if task, ok := byTicket[intent.TaskID]; ok {
			if err := l.refreshOne(ctx, task, rc, now); err != nil {
				return err
			}
		}
		if err := l.store.ConsumeVerbIntent(ctx, intent.ID, now); err != nil {
			return err
		}
	}

	return l.autoRefresh(ctx, tasks, rc, requested, now)
}

// autoRefresh is the automatic pass: every pushed task whose recorded base is stacked and whose
// tip has moved past what was recorded, that a live run or an unresolved merge does not already
// bar (inv. 4) and that has no outstanding refresh_refused fact -- a refusal is never retried
// automatically, exactly as a push failure is not (push.go).
func (l *Loop) autoRefresh(
	ctx context.Context, tasks []Task, rc refreshContext, requested map[string]bool, now time.Time,
) error {
	latest, err := l.store.LatestRunsByTask(ctx)
	if err != nil {
		return err
	}
	pushRows, err := l.store.LatestPushes(ctx)
	if err != nil {
		return err
	}
	refreshFacts, err := l.store.RefreshFacts(ctx)
	if err != nil {
		return err
	}

	for _, t := range tasks {
		if requested[t.TicketURL] {
			continue // already handled above, this same tick
		}
		summary, ok := latest[t.TicketURL]
		if !ok || !summary.HasOutcome || summary.Outcome != plan.OutcomePush {
			continue
		}
		if rc.obs.PRs[t.Branch].State != gh.Open {
			continue
		}
		pushRow, pushed := pushRows[t.TicketURL]
		if !pushed || !baseMoved(pushRow, rc.obs) {
			continue
		}
		if refreshFacts[t.TicketURL].Refused {
			continue
		}
		if err := l.refreshOne(ctx, t, rc, now); err != nil {
			return err
		}
	}
	return nil
}

// baseMoved is the git-level fact §4a marks a row on: a stacked base (never the default branch)
// whose current tip differs from what was recorded at the task's last push.
func baseMoved(row PushRow, obs Observation) bool {
	return row.BaseBranch != "" && row.BaseBranch != defaultBaseBranch &&
		obs.BranchTips[row.BaseBranch] != row.BaseSHAAtPush
}

// refreshOne runs §4a's contract for the non-conflict path, in order, over one task's worktree:
//
//  1. Precondition -- no live run and no unresolved merge (invariant 4). Either failing is a
//     silent no-op, deliberately: there is no event kind for it, and inv. 4 means there is
//     nothing here to record -- the row's own state already says why nothing happened.
//  2. `git merge --ff-only origin/<branch>`. Not a fast-forward -- a genuinely divergent push to
//     the app's own head branch, not the common case -- records refresh_refused and stops; the
//     app never rewrites history.
//  3. `git merge origin/<base_branch>`, base recomputed by the same plan.Unlocked call the push
//     step uses. A conflict leaves the worktree mid-merge for a human with no event needed
//     (`refresh conflicted` is a separate, blocked ticket); success is recorded, and the existing
//     push step delivers the merge commit this same tick, since the local tip now differs from
//     pushes.pushed_tip.
func (l *Loop) refreshOne(ctx context.Context, task Task, rc refreshContext, now time.Time) error {
	branch := task.Branch
	worktreePath, ok := rc.obs.Worktrees[branch]
	if !ok || rc.obs.Runs[task.TicketURL].Alive || rc.obs.MidMerge[branch] {
		return nil
	}

	if err := MergeFFOnly(ctx, worktreePath, "origin/"+branch); err != nil {
		return l.store.AppendEvent(ctx, Event{
			At: now, TaskURL: task.TicketURL, Kind: eventRefreshRefused, Detail: err.Error(),
		})
	}

	unlock := plan.Unlocked(rc.byURL[task.TicketURL], rc.byURL, rc.prs, rc.stacking[task.Repo])
	if !unlock.Unlocked {
		return nil // its blocker's PR closed since the base moved; nothing sane to merge against
	}
	if err := Merge(ctx, worktreePath, "origin/"+unlock.BaseBranch); err != nil {
		return nil // conflict: left mid-merge for a human (refresh conflicted is a separate, blocked ticket)
	}

	return l.store.AppendEvent(ctx, Event{
		At: now, TaskURL: task.TicketURL, Kind: eventRefreshed,
		Detail: fmt.Sprintf("merged origin/%s then origin/%s", branch, unlock.BaseBranch),
	})
}
