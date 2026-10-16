package loop

import (
	"context"
	"fmt"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

type RealClock struct{}

func (RealClock) Now() time.Time                         { return time.Now() }
func (RealClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

var transitionEvents = map[plan.TransitionKind]store.EventKind{
	plan.TransitionOpened:       store.EventPROpened,
	plan.TransitionMerged:       store.EventPRMerged,
	plan.TransitionChecksLanded: store.EventChecksLanded,
	plan.TransitionVerdict:      store.EventVerdictTransition,
}

func (l *Loop) recordTransitions(ctx context.Context, snap plan.Snapshot) error {
	verdictChanged := false
	for _, tr := range snap.Transitions() {
		kind := transitionEvents[tr.Kind]
		ev := store.Event{At: tr.At, TicketURL: string(tr.Ticket), Kind: kind, Detail: tr.Detail}
		if err := l.store.AppendEvent(ctx, ev); err != nil {
			return fmt.Errorf("record %s for %s: %w", kind, tr.Ticket, err)
		}
		verdictChanged = verdictChanged || tr.Kind == plan.TransitionVerdict
	}
	if !verdictChanged {
		return nil
	}
	saved := make(map[string]string, len(snap.Verdicts()))
	for url, v := range snap.Verdicts() {
		saved[string(url)] = v
	}
	if err := l.store.SaveLastVerdicts(ctx, saved); err != nil {
		return fmt.Errorf("save last verdicts: %w", err)
	}
	return nil
}

func (l *Loop) applyDraftGate(ctx context.Context, snap plan.Snapshot) error {
	return eachEntry(snap, func(e plan.Entry) bool { return e.ReadyToUndraft }, func(e plan.Entry) error {
		return l.readyOne(ctx, e.Ticket)
	})
}

func (l *Loop) readyOne(ctx context.Context, t plan.Ticket) error {
	if err := l.forge.Ready(ctx, l.checkout(t.Repo), t.Branch); err != nil {
		return l.event(ctx, string(t.URL), store.EventDraftReadyFailed, err.Error())
	}
	return l.event(ctx, string(t.URL), store.EventDraftReady, "")
}

// Mergify's queue takes main-based pull requests only, and both repos delete a merged branch.
func (l *Loop) retargetMerged(ctx context.Context, snap plan.Snapshot, obs plan.Observation) error {
	return eachEntry(snap, func(e plan.Entry) bool {
		t := e.Ticket
		if e.HoldsWorktree() || e.LastPush.IsNone() {
			return false
		}
		row := e.LastPush.Unwrap()
		if row.BaseBranch == "" || row.BaseBranch == plan.DefaultBaseBranch {
			return false
		}
		return obs.PRs[t.Ref()].State == plan.Open &&
			obs.PRs[plan.BranchRef{Repo: t.Repo, Branch: row.BaseBranch}].State == plan.Merged
	}, func(e plan.Entry) error {
		return l.retargetOne(ctx, snap, obs, e.Ticket, e.LastPush.Unwrap())
	})
}

func (l *Loop) retargetOne(
	ctx context.Context, snap plan.Snapshot, obs plan.Observation, t plan.Ticket, row plan.PushRow,
) error {
	url := string(t.URL)
	repoPath := l.checkout(t.Repo)
	if err := l.forge.Edit(ctx, repoPath, t.Branch, plan.DefaultBaseBranch); err != nil {
		return l.event(ctx, url, store.EventRetargetFailed, err.Error())
	}

	err := l.store.RecordPush(ctx, store.Push{
		Ticket: t.URL, Tip: row.PushedTip, BaseSHA: row.BaseSHAAtPush, BaseBranch: plan.DefaultBaseBranch,
		At: l.clock.Now(),
	})
	if err != nil {
		return fmt.Errorf("record retargeted push: %w", err)
	}
	detail := fmt.Sprintf("re-pointed %s from %s at %s, which merged", t.Branch, row.BaseBranch, plan.DefaultBaseBranch)
	if err := l.event(ctx, url, store.EventRetargeted, detail); err != nil {
		return fmt.Errorf("record retarget: %w", err)
	}
	if err := l.refreshOne(ctx, snap, obs, t, row, false); err != nil {
		return fmt.Errorf("refresh after retarget: %w", err)
	}
	return nil
}
