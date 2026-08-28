package cc

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/gh"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

const refreshVerb = plan.VerbRefresh

const (
	eventRefreshRefused    = "refresh_refused"
	eventRefreshConflicted = "refresh_conflicted"
	eventRefreshed         = "refreshed"
	eventRestacked         = "restacked"
)

// RefreshFact is a ticket's outstanding refused fast-forward, derived from the latest
// refresh_refused event since its last recorded push, so the next push clears it
// (docs/designs/command-centre-design.md § 4a).
type RefreshFact struct {
	Refused bool
	Reason  string
}

// RefreshFacts returns every ticket's outstanding refused fast-forward, keyed by ticket URL.
// A refusal gates the automatic pass's retry; the refresh verb ignores it
// (docs/designs/command-centre-design.md § 4a).
func (s *Store) RefreshFacts(ctx context.Context) (map[string]RefreshFact, error) {
	outcomes, err := s.latestRefreshOutcomes(ctx)
	if err != nil {
		return nil, err
	}
	facts := make(map[string]RefreshFact, len(outcomes))
	for ticketID, o := range outcomes {
		if o.kind == eventRefreshRefused {
			facts[ticketID] = RefreshFact{Refused: true, Reason: o.detail}
		}
	}
	return facts, nil
}

type refreshOutcome struct{ kind, detail string }

// latestRefreshOutcomes returns each ticket's latest refresh outcome the automatic pass never
// retries, keyed by ticket URL, and the push that clears it
// (docs/designs/command-centre-design.md § 4a).
func (s *Store) latestRefreshOutcomes(ctx context.Context) (map[string]refreshOutcome, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.ticket_id, e.kind, e.detail
		FROM events e
		JOIN (
			SELECT e2.ticket_id, MAX(e2.id) AS id
			FROM events e2
			LEFT JOIN (
				SELECT ticket_id, MAX(pushed_at) AS pushed_at FROM pushes GROUP BY ticket_id
			) p ON p.ticket_id = e2.ticket_id
			WHERE e2.kind IN (?, ?) AND e2.at > COALESCE(p.pushed_at, '')
			GROUP BY e2.ticket_id
		) latest ON latest.ticket_id = e.ticket_id AND latest.id = e.id`,
		eventRefreshRefused, eventRefreshConflicted)
	if err != nil {
		return nil, fmt.Errorf("select refresh outcomes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	outcomes := map[string]refreshOutcome{}
	for rows.Next() {
		var ticketID, kind string
		var detail sql.NullString
		if err := rows.Scan(&ticketID, &kind, &detail); err != nil {
			return nil, fmt.Errorf("scan refresh outcome: %w", err)
		}
		outcomes[ticketID] = refreshOutcome{kind: kind, detail: detail.String}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate refresh outcomes: %w", err)
	}
	return outcomes, nil
}

type refreshContext struct {
	byURL     map[string]plan.Ticket
	stacking  map[string]bool
	prs       map[string]plan.PRState
	repoPaths map[string]string
	pushRows  map[string]PushRow
	obs       Observation
}

func (l *Loop) newRefreshContext(ctx context.Context, tickets []Ticket, obs Observation) (refreshContext, error) {
	pushRows, err := l.store.LatestPushes(ctx)
	if err != nil {
		return refreshContext{}, err
	}
	return refreshContext{
		byURL:     planTicketsByURL(tickets),
		stacking:  stackingByRepo(l.cfg.Repos),
		prs:       prsByBranch(obs),
		repoPaths: repoPathsByName(l.cfg.Repos),
		pushRows:  pushRows,
		obs:       obs,
	}, nil
}

// applyRefreshIntents runs every requested refresh, which bypasses the RefreshFacts gate the way
// retry-push bypasses PushFacts, then sweeps the eligible base-moved rows
// (docs/designs/command-centre-design.md § 4a).
func (l *Loop) applyRefreshIntents(ctx context.Context, obs Observation) error {
	tickets, err := l.store.Tickets(ctx)
	if err != nil {
		return err
	}
	byTicket := ticketsByURL(tickets)
	rc, err := l.newRefreshContext(ctx, tickets, obs)
	if err != nil {
		return err
	}
	now := l.now()

	intents, err := l.store.PendingVerbIntents(ctx, refreshVerb)
	if err != nil {
		return err
	}
	requested := make(map[string]bool, len(intents))
	for _, intent := range intents {
		requested[intent.TicketID] = true
		if ticket, ok := byTicket[intent.TicketID]; ok {
			if err := l.refreshOne(ctx, ticket, rc.pushRows[ticket.URL], rc, now); err != nil {
				return err
			}
		}
		if err := l.store.ConsumeVerbIntent(ctx, intent.ID, now); err != nil {
			return err
		}
	}

	return l.autoRefresh(ctx, tickets, rc, requested, now)
}

// autoRefresh sweeps every pushed, base-moved row that no live run or unresolved merge bars
// (inv. 4) and whose last refresh neither refused nor conflicted, so a human's abort is not
// undone by the next tick re-running the same merge (docs/designs/command-centre-design.md § 4a).
func (l *Loop) autoRefresh(
	ctx context.Context, tickets []Ticket, rc refreshContext, requested map[string]bool, now time.Time,
) error {
	latest, err := l.store.LatestRunsByTicket(ctx)
	if err != nil {
		return err
	}
	outcomes, err := l.store.latestRefreshOutcomes(ctx)
	if err != nil {
		return err
	}

	for _, t := range tickets {
		if requested[t.URL] {
			continue
		}
		summary, ok := latest[t.URL]
		if !ok || !summary.HasOutcome || summary.Outcome != plan.OutcomePush {
			continue
		}
		if rc.obs.PRs[t.Branch].State != gh.Open {
			continue
		}
		pushRow, pushed := rc.pushRows[t.URL]
		if !pushed || !baseMoved(pushRow, rc.obs, t.Repo) {
			continue
		}
		// ponytail: the gate holds until the row's next push, so a later, cleanly-mergeable base
		// advance also waits for the refresh verb. Compare the conflict's own base tip if that bites.
		if _, tried := outcomes[t.URL]; tried {
			continue
		}
		if err := l.refreshOne(ctx, t, pushRow, rc, now); err != nil {
			return err
		}
	}
	return nil
}

// baseMoved is the git-level fact §4a marks a row on: the row's recorded base -- a stacked
// branch, or main once retargetMerged has pointed it there -- whose current tip differs from
// what was recorded at the ticket's last push (issue #85: main counts the same as a stacked base).
func baseMoved(row PushRow, obs Observation, repo string) bool {
	return row.BaseBranch != "" && obs.BranchTips[baseTipKey(repo, row.BaseBranch)] != row.BaseSHAAtPush
}

// refreshOne fast-forwards one ticket's own branch, then advances it onto its base. A refused
// fast-forward records refresh_refused and stops; a conflict is left mid-merge for a human
// (docs/designs/command-centre-design.md § 4a).
func (l *Loop) refreshOne(
	ctx context.Context, ticket Ticket, row PushRow, rc refreshContext, now time.Time,
) error {
	branch := ticket.Branch
	worktreePath, ok := rc.obs.Worktrees[branch]
	if !ok || rc.obs.Runs[ticket.URL].Alive || rc.obs.MidMerge[branch] {
		return nil
	}

	if err := MergeFFOnly(ctx, worktreePath, "origin/"+branch); err != nil {
		return l.store.AppendEvent(ctx, Event{
			At: now, TicketURL: ticket.URL, Kind: eventRefreshRefused, Detail: err.Error(),
		})
	}

	unlock := plan.Unlocked(rc.byURL[ticket.URL], rc.byURL, rc.prs, rc.stacking[ticket.Repo])
	if !unlock.Unlocked {
		return nil // its blocker's PR closed since the base moved; nothing sane to merge against
	}
	restacked, detail, err := advanceOnto(ctx, worktreePath, unlock.BaseBranch, row, rc.obs)
	if err != nil {
		// A rebase that stops on a conflict has already rewritten the branch, so the push after
		// whoever resolves it still needs the lease the completed restack would have earned
		// (issue #93). A conflicted merge rewrites nothing and earns nothing.
		if restacked {
			if err := l.store.AppendEvent(ctx, Event{
				At: now, TicketURL: ticket.URL, Kind: eventRestacked,
				Detail: detail + ", conflicted",
			}); err != nil {
				return err
			}
		}
		return l.store.AppendEvent(ctx, Event{
			At: now, TicketURL: ticket.URL, Kind: eventRefreshConflicted, Detail: err.Error(),
		})
	}

	kind := eventRefreshed
	if restacked {
		kind = eventRestacked
	}
	return l.store.AppendEvent(ctx, Event{
		At: now, TicketURL: ticket.URL, Kind: kind,
		Detail: fmt.Sprintf("merged origin/%s then %s", branch, detail),
	})
}

// advanceOnto merges the base when the base only moved forward, and restacks when the base's
// history no longer contains what the branch was built on -- a squash-merged parent, or a base
// the app itself rewrote a tick earlier (issue #89). Merging in that case replays the branch's
// own copies of commits the base already carries under new SHAs, which conflicts on every line
// either side touched. It reports which of the two it did, because only a restack licenses the
// push step to lease-force, and what the event should say.
func advanceOnto(
	ctx context.Context, worktreePath, base string, row PushRow, obs Observation,
) (bool, string, error) {
	ref := "origin/" + base
	boundary := restackBoundary(row, obs)
	if boundary == "" {
		return false, ref, Merge(ctx, worktreePath, ref)
	}
	kept, err := Ancestor(ctx, worktreePath, boundary, ref)
	if err != nil {
		return false, "", err
	}
	if kept {
		return false, ref, Merge(ctx, worktreePath, ref)
	}
	return true, fmt.Sprintf("restacked onto %s, dropping everything up to %s", ref, boundary),
		Rebase(ctx, worktreePath, ref, boundary)
}

// restackBoundary is the last commit of the branch's recorded base that its own commits sit on
// top of, so a restack drops exactly the work the base already carries. A merged base is read
// from its pull request's head, not from base_sha_at_push, because a base that advanced after
// this branch's last push has those later commits in the squash too (issue #89).
func restackBoundary(row PushRow, obs Observation) string {
	if row.BaseBranch == "" {
		return ""
	}
	if pr := obs.PRs[row.BaseBranch]; pr.State == gh.Merged && pr.HeadOid != "" {
		return pr.HeadOid
	}
	return row.BaseSHAAtPush
}
