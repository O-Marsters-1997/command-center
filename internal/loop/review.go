package loop

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

const (
	verbReviewFollowUp = "review-follow-up"
	verbReviewComment  = "review-comment"

	eventReviewQueuedFollowUp = "review_follow_up_queued"
	eventReviewCommented      = "review_commented"
	eventReviewCommentFailed  = "review_comment_failed"
)

func (l *Loop) findingsPath(runID int64) string {
	return filepath.Join(l.ws.RunsDir, fmt.Sprintf("%d.findings.md", runID))
}

func dispositionFor(kind string, commits int) plan.Outcome {
	if kind == runKindReview {
		return plan.OutcomePush
	}
	return plan.Disposition(commits)
}

func (l *Loop) launchReviews(ctx context.Context, snap plan.Snapshot, obs plan.Observation) error {
	latest, err := l.store.LatestRunsByTicket(ctx)
	if err != nil {
		return err
	}
	pushes, err := l.store.LatestPushes(ctx)
	if err != nil {
		return err
	}
	for _, e := range snap.Entries {
		run, ok := latest[e.Ticket.URL]
		if !ok || !run.HasOutcome || run.Outcome != plan.OutcomePush {
			continue
		}
		if run.Kind != runKindAgent && run.Kind != runKindFollowUp {
			continue
		}
		key := plan.BranchKey(e.Ticket.Repo, e.Ticket.Branch)
		tip := obs.LocalTips[key]
		if tip == "" || pushes[e.Ticket.URL].PushedTip != tip || obs.PRs[key].State != plan.Open {
			continue
		}
		if !snap.HasSlotAfter(len(l.spawned)) {
			return nil
		}
		if err := l.reviewOne(ctx, e, tip, obs); err != nil {
			return err
		}
	}
	return nil
}

func (l *Loop) reviewOne(ctx context.Context, e plan.Entry, tip string, obs plan.Observation) error {
	worktreePath, refusal := idleWorktreeFor(e.Ticket, obs)
	if refusal != "" {
		return nil
	}
	base := e.Unlock.BaseBranch
	if base == "" {
		base = plan.DefaultBaseBranch
	}
	ticket, ok, err := l.ticket(ctx, e.Ticket.URL)
	if err != nil || !ok {
		return err
	}
	return l.spawnRun(ctx, spawnSpec{
		ticket: ticket, worktree: worktreePath, baseline: tip, kind: runKindReview,
		promptFor: func(runID int64) string { return plan.ComposeReview(base, l.findingsPath(runID)) },
	})
}

func (l *Loop) queueReviewFindings(ctx context.Context, run store.PendingRun) error {
	data, err := os.ReadFile(l.findingsPath(run.ID))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read findings for run %d: %w", run.ID, err)
	}
	findings := strings.TrimSpace(string(data))
	if findings == "" {
		return nil
	}

	reviewed, err := l.store.PrecedingRunKind(ctx, run.TicketID, run.ID)
	if err != nil {
		return err
	}
	verb := verbReviewFollowUp
	if reviewed == runKindFollowUp {
		verb = verbReviewComment
	}
	return l.store.QueueVerbIntentWithPayload(ctx, run.TicketID, verb, findings, l.clock.Now())
}

func (l *Loop) applyReviewFindingsIntents(ctx context.Context, obs plan.Observation) error {
	err := l.eachIntent(ctx, verbReviewFollowUp, func(intent store.VerbIntent) error {
		ticket, ok, err := l.ticket(ctx, intent.TicketID)
		if err != nil || !ok {
			return err
		}
		if err := l.followUpOne(ctx, ticket, intent.Payload, obs); err != nil {
			return err
		}
		return l.event(ctx, ticket.URL, eventReviewQueuedFollowUp, "")
	})
	if err != nil {
		return err
	}
	return l.eachIntent(ctx, verbReviewComment, func(intent store.VerbIntent) error {
		ticket, ok, err := l.ticket(ctx, intent.TicketID)
		if err != nil || !ok {
			return err
		}
		body := "Review findings left unfixed:\n\n" + intent.Payload
		if err := l.forge.Comment(ctx, l.checkout(ticket.Repo), ticket.Branch, body); err != nil {
			return l.event(ctx, ticket.URL, eventReviewCommentFailed, err.Error())
		}
		return l.event(ctx, ticket.URL, eventReviewCommented, "")
	})
}
