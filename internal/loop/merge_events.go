package loop

import (
	"context"
	"fmt"
	"log"

	"github.com/O-Marsters-1997/command-center/internal/git"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

const eventPRMerged = "pr_merged"

func (l *Loop) recordMergedEvents(ctx context.Context, obs plan.Observation) error {
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
		if pr.State != plan.Merged {
			continue
		}
		exists, err := l.store.HasEvent(ctx, t.URL, eventPRMerged)
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		l.recordHandChurn(ctx, t, pr, pushes[t.URL], repoPaths[t.Repo])
		if err := l.store.AppendEvent(ctx, store.Event{
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
func (l *Loop) recordHandChurn(ctx context.Context, t store.Ticket, pr plan.PR, push plan.PushRow, repoPath string) {
	if push.PushedTip == "" || repoPath == "" || pr.HeadOid == "" {
		return
	}
	var err error
	if pr.HeadOid == push.PushedTip {
		err = l.store.SetHandChurnLines(ctx, t.URL, 0)
	} else if lines, diffErr := git.LinesChanged(ctx, repoPath, push.PushedTip, pr.HeadOid); diffErr != nil {
		log.Printf("hand churn for %s: %v", t.URL, diffErr)
		return
	} else {
		err = l.store.SetHandChurnLines(ctx, t.URL, lines)
	}
	if err != nil {
		log.Printf("hand churn for %s: %v", t.URL, err)
	}
}
