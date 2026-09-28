package cc

import (
	"context"
	"fmt"

	"github.com/O-Marsters-1997/command-center/internal/gh"
)

const eventPRMerged = "pr_merged"

func (l *Loop) recordMergedEvents(ctx context.Context, obs Observation) error {
	tickets, err := l.store.Tickets(ctx)
	if err != nil {
		return err
	}
	pushes, err := l.store.LatestPushes(ctx)
	if err != nil {
		return err
	}
	repoPaths := repoPathsByName(l.cfg.Repos)

	for _, t := range tickets {
		pr := obs.PRs[branchKey(t.Repo, t.Branch)]
		if pr.State != gh.Merged {
			continue
		}
		exists, err := l.store.HasEvent(ctx, t.URL, eventPRMerged)
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		if err := l.recordHandChurn(ctx, t, pr, pushes[t.URL], repoPaths[t.Repo]); err != nil {
			return err
		}
		if err := l.store.AppendEvent(ctx, Event{
			At: pr.MergedAt, TicketURL: t.URL, Kind: eventPRMerged,
			Detail: fmt.Sprintf("PR #%d merged", pr.Number),
		}); err != nil {
			return err
		}
	}
	return nil
}

// recordHandChurn diffs push.PushedTip (pushOne's own last write, cc's last commit) against the
// merged PR's head, leaving hand_churn_lines NULL rather than 0 when there is nothing to diff.
func (l *Loop) recordHandChurn(ctx context.Context, t Ticket, pr gh.PR, push PushRow, repoPath string) error {
	if push.PushedTip == "" || repoPath == "" || pr.HeadOid == "" {
		return nil
	}
	if pr.HeadOid == push.PushedTip {
		return l.store.SetHandChurnLines(ctx, t.URL, 0)
	}
	lines, err := LinesChanged(ctx, repoPath, push.PushedTip, pr.HeadOid)
	if err != nil {
		return fmt.Errorf("hand churn for %s: %w", t.URL, err)
	}
	return l.store.SetHandChurnLines(ctx, t.URL, lines)
}
