package plan

import (
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/option"
	"github.com/O-Marsters-1997/command-center/internal/verdict"
)

type TransitionKind string

const (
	TransitionOpened       TransitionKind = "opened"
	TransitionMerged       TransitionKind = "merged"
	TransitionChecksLanded TransitionKind = "checks_landed"
	TransitionVerdict      TransitionKind = "verdict"
)

type Transition struct {
	Ticket TicketURL
	Kind   TransitionKind
	At     time.Time
	Detail string
}

func (s Snapshot) Transitions() []Transition { return s.transitions }

func (s Snapshot) Verdicts() map[TicketURL]string { return s.verdicts }

func (r Rules) transitions(in Input) ([]Transition, map[TicketURL]string) {
	verdicts := maps.Clone(in.LastVerdicts)
	if verdicts == nil {
		verdicts = map[TicketURL]string{}
	}
	var out []Transition
	for _, t := range in.Tickets {
		out = append(out, openedTransition(t, in)...)
		out = append(out, checksLandedTransitions(t, in)...)
		if tr, current, ok := r.verdictTransition(t, in); ok {
			verdicts[t.URL] = current
			out = append(out, tr)
		}
		out = append(out, mergedTransition(t, in)...)
	}
	return out, verdicts
}

func openedTransition(t Ticket, in Input) []Transition {
	pr, observed := in.Obs.PRs[t.Ref()]
	if _, seen := in.Prev.PRs[t.Ref()]; !observed || seen || in.Recorded[t.URL].Opened {
		return nil
	}
	detail := fmt.Sprintf("PR #%d https://github.com/%s/pull/%d", pr.Number, t.Repo, pr.Number)
	return []Transition{{Ticket: t.URL, Kind: TransitionOpened, At: in.Now, Detail: detail}}
}

func mergedTransition(t Ticket, in Input) []Transition {
	pr := in.Obs.PRs[t.Ref()]
	if pr.State != Merged || in.Recorded[t.URL].Merged {
		return nil
	}
	return []Transition{{
		Ticket: t.URL, Kind: TransitionMerged, At: pr.MergedAt, Detail: fmt.Sprintf("PR #%d merged", pr.Number),
	}}
}

func checksLandedTransitions(t Ticket, in Input) []Transition {
	now := in.Obs.PRs[t.Ref()].Checks
	before := in.Prev.PRs[t.Ref()].Checks
	var out []Transition
	for _, name := range slices.Sorted(maps.Keys(now)) {
		landed := ToVerdictCheckState(now[name])
		if landed == verdict.Pending {
			continue
		}
		if was, ok := before[name]; ok && ToVerdictCheckState(was) != verdict.Pending {
			continue
		}
		out = append(out, Transition{
			Ticket: t.URL, Kind: TransitionChecksLanded, At: in.Now, Detail: name + " " + landed.String(),
		})
	}
	return out
}

func (r Rules) verdictTransition(t Ticket, in Input) (Transition, string, bool) {
	if in.Runs[t.URL].Outcome != option.Some(OutcomePush) {
		return Transition{}, "", false
	}
	pf := in.Pushes[t.URL]
	if pf.Refused || pf.Failed || in.Obs.PRs[t.Ref()].State != Open {
		return Transition{}, "", false
	}
	result := r.ApplyVerdict(t, in.Obs, in.Verdict)
	if result.IsNone() {
		return Transition{}, "", false
	}
	current := result.Unwrap().Kind.String()
	if current == "" || in.LastVerdicts[t.URL] == current {
		return Transition{}, "", false
	}
	return Transition{
		Ticket: t.URL, Kind: TransitionVerdict, At: in.Now, Detail: current + ": " + result.Unwrap().Reason,
	}, current, true
}
