package loop

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/O-Marsters-1997/command-center/internal/adapter/git"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

func (l *Loop) pushPushable(ctx context.Context, snap plan.Snapshot, obs plan.Observation) error {
	for _, p := range snap.Pushable(obs) {
		e, _ := snap.Entry(p.Ticket)
		if _, err := l.pushOne(ctx, e, p, obs); err != nil {
			return fmt.Errorf("%s/%s: %w", e.Ticket.Repo, e.Ticket.Branch, err)
		}
	}
	return nil
}

func (l *Loop) applyRetryPushIntents(ctx context.Context, snap plan.Snapshot, obs plan.Observation) error {
	return l.eachIntentEntry(ctx, snap, plan.VerbRetryPush, func(intent plan.Intent, e plan.Entry) (handled, error) {
		p, ok := snap.Push(intent.Ticket, obs)
		if !ok {
			return failedWith("there is nothing to push"), nil
		}
		return l.pushOne(ctx, e, p, obs)
	})
}

func (l *Loop) applyRaiseIntents(ctx context.Context, snap plan.Snapshot, obs plan.Observation) error {
	return l.eachIntentEntry(ctx, snap, plan.VerbRaise, func(intent plan.Intent, e plan.Entry) (handled, error) {
		url := string(intent.Ticket)
		if e.State != plan.AwaitingPR {
			return l.refuse(ctx, url, store.EventRaiseRefused, fmt.Sprintf("the ticket is %s, not awaiting PR", e.State))
		}
		p, ok := snap.Push(intent.Ticket, obs)
		if !ok || string(p.LocalTip) != intent.Payload {
			return l.refuse(ctx, url, store.EventRaiseRefused, "the tip has moved since you reviewed it")
		}
		return l.pushOne(ctx, e, p, obs)
	})
}

func (l *Loop) applyAdoptHeadIntents(ctx context.Context, snap plan.Snapshot, obs plan.Observation) error {
	return l.eachIntentEntry(ctx, snap, plan.VerbAdoptHead, func(intent plan.Intent, e plan.Entry) (handled, error) {
		pr := obs.PRs[e.Ticket.Ref()]
		if pr.State != plan.Open || pr.HeadOid == "" {
			return failedWith("the ticket has no open PR"), nil
		}
		if worktree := obs.Worktrees[e.Ticket.Ref()]; worktree != "" && obs.HeadDescends[e.Ticket.Ref()] {
			if err := git.MergeFFOnly(ctx, worktree, "origin/"+e.Ticket.Branch); err != nil {
				return handled{}, fmt.Errorf("bring %s up to its remote head: %w", e.Ticket.Branch, err)
			}
		}
		base := e.Unlock.BaseBranch
		baseSHA, err := git.RevParse(ctx, l.checkout(e.Ticket.Repo), "origin/"+base)
		if err != nil {
			return handled{}, fmt.Errorf("resolve origin/%s: %w", base, err)
		}
		push := store.Push{
			Ticket: intent.Ticket, Tip: pr.HeadOid, BaseSHA: plan.SHA(baseSHA), BaseBranch: base, At: l.clock.Now(),
		}
		if e.LastPush.IsSome() {
			push.Protected = e.LastPush.Unwrap().Protected
		}
		if err := l.store.RecordPush(ctx, push); err != nil {
			return handled{}, fmt.Errorf("record adopted head of %s: %w", e.Ticket.Branch, err)
		}
		adopted := fmt.Sprintf("adopted remote head %s", pr.HeadOid)
		return succeeded, l.event(ctx, string(intent.Ticket), store.EventPushed, adopted)
	})
}

func sameProtectedPaths(now []plan.PathMatch, last option.Option[plan.PushRow]) bool {
	var before []plan.PathMatch
	if last.IsSome() {
		before = last.Unwrap().Protected
	}
	paths := func(ms []plan.PathMatch) []string {
		out := make([]string, len(ms))
		for i, m := range ms {
			out[i] = m.Path
		}
		slices.Sort(out)
		return out
	}
	return slices.Equal(paths(now), paths(before))
}

func pushBranch(ctx context.Context, repoPath, branch string, tip plan.SHA, restacked bool) error {
	recordedTip := string(tip)
	if !restacked || recordedTip == "" {
		return wrapGit("push "+branch, git.Push(ctx, repoPath, branch))
	}
	descended, err := git.Ancestor(ctx, repoPath, recordedTip, branch)
	if err != nil {
		return fmt.Errorf("check %s descends from %s: %w", branch, recordedTip, err)
	}
	if descended {
		return wrapGit("push "+branch, git.Push(ctx, repoPath, branch))
	}
	return wrapGit("force-push restacked "+branch, git.PushRestacked(ctx, repoPath, branch, recordedTip))
}

func classifyChanges(
	ctx context.Context, repoPath, base, branch string, settings plan.RepoSettings,
) ([]plan.PathMatch, map[string]git.FileStat, error) {
	stats, err := git.DiffStat(ctx, repoPath, "origin/"+base, branch)
	if err != nil {
		return nil, nil, fmt.Errorf("diff %s against origin/%s: %w", branch, base, err)
	}
	changed := make([]string, len(stats))
	counts := make(map[string]git.FileStat, len(stats))
	for i, s := range stats {
		changed[i] = s.Path
		counts[s.Path] = s
	}
	policy := plan.Policy{Deny: settings.Deny, Protect: settings.Protect}
	return plan.Classify(changed, policy), counts, nil
}

func (l *Loop) pushOne(ctx context.Context, e plan.Entry, p plan.Push, obs plan.Observation) (handled, error) {
	t := e.Ticket
	url := string(t.URL)
	base := e.Unlock.BaseBranch
	repoPath := l.checkout(t.Repo)

	matches, counts, err := classifyChanges(ctx, repoPath, base, t.Branch, obs.Settings[t.Repo])
	if err != nil {
		return handled{}, err
	}
	if denied := plan.Denied(matches); len(denied) > 0 {
		detail, err := json.Marshal(denied)
		if err != nil {
			return handled{}, fmt.Errorf("encode denied paths: %w", err)
		}
		return l.refuse(ctx, url, store.EventPushRefused, string(detail))
	}

	var lastPushed plan.SHA
	if e.LastPush.IsSome() {
		lastPushed = e.LastPush.Unwrap().PushedTip
	}
	if err := pushBranch(ctx, repoPath, t.Branch, lastPushed, p.Force); err != nil {
		return l.refuse(ctx, url, store.EventPushFailed, err.Error())
	}

	protected := plan.Protected(matches)
	for i, m := range protected {
		protected[i].Added, protected[i].Deleted = counts[m.Path].Added, counts[m.Path].Deleted
	}
	section := plan.ProtectedSection(protected)

	if obs.PRs[t.Ref()].State != plan.Open {
		body := l.prBody(ctx, t.URL, p.Body)
		var labels []string
		if section != "" {
			body = strings.TrimSpace(body + "\n\n" + section)
			labels = []string{plan.ProtectedLabel}
		}
		worktree := obs.Worktrees[t.Ref()]
		if err := l.forge.Create(ctx, worktree, base, body, p.OpensAsDraft, labels); err != nil {
			return l.refuse(ctx, url, store.EventPushFailed, err.Error())
		}
	} else if section != "" && !sameProtectedPaths(protected, e.LastPush) {
		if err := l.forge.Comment(ctx, repoPath, t.Branch, section); err != nil {
			return l.refuse(ctx, url, store.EventPushFailed, err.Error())
		}
	}

	baseSHA, err := git.RevParse(ctx, repoPath, "origin/"+base)
	if err != nil {
		return handled{}, fmt.Errorf("resolve origin/%s: %w", base, err)
	}
	push := store.Push{
		Ticket: plan.TicketURL(url), Tip: p.LocalTip, BaseSHA: plan.SHA(baseSHA), BaseBranch: base, At: l.clock.Now(),
		Protected: protected,
	}
	if err := l.store.RecordPush(ctx, push); err != nil {
		return handled{}, fmt.Errorf("record push of %s: %w", t.Branch, err)
	}
	detail := fmt.Sprintf("pushed %s to %s (base %s)", p.LocalTip, t.Branch, base)
	return succeeded, l.event(ctx, url, store.EventPushed, detail)
}
