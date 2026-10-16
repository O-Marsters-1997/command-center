package loop

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/option"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/verdict"
)

type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

type RealClock struct{}

func (RealClock) Now() time.Time                         { return time.Now() }
func (RealClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

func (l *Loop) recordOpenedEvents(ctx context.Context, prev, obs plan.Observation, tickets []store.Ticket) error {
	for _, t := range tickets {
		key := t.Ref()
		pr, observed := obs.PRs[key]
		if _, seen := prev.PRs[key]; !observed || seen {
			continue
		}
		exists, err := l.store.HasEvent(ctx, t.URL, store.EventPROpened)
		if err != nil {
			return fmt.Errorf("check pr_opened event for %s: %w", t.URL, err)
		}
		if exists {
			continue
		}
		detail := fmt.Sprintf("PR #%d https://github.com/%s/pull/%d", pr.Number, t.Repo, pr.Number)
		if err := l.event(ctx, t.URL, store.EventPROpened, detail); err != nil {
			return fmt.Errorf("record pr_opened for %s: %w", t.URL, err)
		}
	}
	return nil
}

func (l *Loop) recordMergedEvents(ctx context.Context, obs plan.Observation, tickets []store.Ticket) error {
	for _, t := range tickets {
		pr := obs.PRs[t.Ref()]
		if pr.State != plan.Merged {
			continue
		}
		exists, err := l.store.HasEvent(ctx, t.URL, store.EventPRMerged)
		if err != nil {
			return fmt.Errorf("check pr_merged event for %s: %w", t.URL, err)
		}
		if exists {
			continue
		}
		if err := l.store.AppendEvent(ctx, store.Event{
			At: pr.MergedAt, TicketURL: t.URL, Kind: store.EventPRMerged,
			Detail: fmt.Sprintf("PR #%d merged", pr.Number),
		}); err != nil {
			return fmt.Errorf("record pr_merged for %s: %w", t.URL, err)
		}
	}
	return nil
}

func (l *Loop) recordChecksLanded(ctx context.Context, prev, obs plan.Observation, tickets []store.Ticket) error {
	for _, t := range tickets {
		key := t.Ref()
		now := obs.PRs[key].Checks
		before := prev.PRs[key].Checks
		for _, name := range slices.Sorted(maps.Keys(now)) {
			landed := plan.ToVerdictCheckState(now[name])
			if landed == verdict.Pending {
				continue
			}
			if was, ok := before[name]; ok && plan.ToVerdictCheckState(was) != verdict.Pending {
				continue
			}
			if err := l.event(ctx, t.URL, store.EventChecksLanded, name+" "+landed.String()); err != nil {
				return fmt.Errorf("record check %s for %s: %w", name, t.URL, err)
			}
		}
	}
	return nil
}

func (l *Loop) recordVerdictTransitions(ctx context.Context, obs plan.Observation, tickets []store.Ticket) error {
	latest, err := l.store.LatestRunsByTicket(ctx)
	if err != nil {
		return fmt.Errorf("latest runs: %w", err)
	}
	pushFacts, err := l.store.PushFacts(ctx)
	if err != nil {
		return fmt.Errorf("push facts: %w", err)
	}
	lastVerdicts, err := l.store.LastVerdicts(ctx)
	if err != nil {
		return fmt.Errorf("last verdicts: %w", err)
	}
	vd, err := l.store.VerdictFacts(ctx)
	if err != nil {
		return fmt.Errorf("verdict facts: %w", err)
	}
	changed := false
	for _, t := range tickets {
		url := plan.TicketURL(t.URL)
		summary, ok := latest[url]
		if !ok || summary.Outcome != option.Some(plan.OutcomePush) {
			continue
		}
		pf := pushFacts[url]
		if pf.Refused || pf.Failed || obs.PRs[t.Ref()].State != plan.Open {
			continue
		}
		result := l.rules(obs).ApplyVerdict(t.Plan(), obs, vd)
		if result.IsNone() {
			continue
		}
		current := result.Unwrap().Kind.String()
		if current == "" || lastVerdicts[t.URL] == current {
			continue
		}
		lastVerdicts[t.URL] = current
		changed = true
		if err := l.event(ctx, t.URL, store.EventVerdictTransition, current+": "+result.Unwrap().Reason); err != nil {
			return fmt.Errorf("record verdict transition for %s: %w", t.URL, err)
		}
	}
	if !changed {
		return nil
	}
	if err := l.store.SaveLastVerdicts(ctx, lastVerdicts); err != nil {
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
