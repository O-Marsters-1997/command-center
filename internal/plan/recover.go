package plan

import (
	"fmt"
	"maps"
	"slices"

	"github.com/O-Marsters-1997/command-center/internal/plan/verdict"
)

const MaxAutoRounds = 2

type FollowUpCause string

const (
	CauseNone         FollowUpCause = ""
	CauseCIFailed     FollowUpCause = "ci_failed"
	CauseVerifyFailed FollowUpCause = "verify_failed"
)

func AllFollowUpCauses() []FollowUpCause { return []FollowUpCause{CauseCIFailed, CauseVerifyFailed} }

type AutoHistory struct {
	Rounds  int
	LastTip SHA
}

type AutoFollowUp struct {
	Ticket  TicketURL
	Cause   FollowUpCause
	Tip     SHA
	LogPath string
}

func (s Snapshot) AutoFollowUps() []AutoFollowUp {
	var out []AutoFollowUp
	for _, e := range s.Entries {
		auto, ok := s.autoCandidate(e)
		if !ok {
			continue
		}
		history := s.auto[e.Ticket.URL]
		if history.Rounds >= MaxAutoRounds || history.LastTip == auto.Tip || len(s.intents[e.Ticket.URL]) > 0 {
			continue
		}
		out = append(out, auto)
	}
	return out
}

func (s Snapshot) autoCandidate(e Entry) (AutoFollowUp, bool) {
	if e.State == VerificationFailed {
		fact, ok := s.verifyFacts[e.Ticket.URL]
		return AutoFollowUp{Ticket: e.Ticket.URL, Cause: CauseVerifyFailed, Tip: fact.Tip, LogPath: fact.LogPath}, ok
	}
	if e.State != CIFailed || e.Run.IsNone() || e.Run.Unwrap().Verdict.IsNone() || e.LastPush.IsNone() {
		return AutoFollowUp{}, false
	}
	red := e.Run.Unwrap().Verdict.Unwrap().RedLeaves
	checks := s.obs.PRs[e.Ticket.Ref()].Checks
	if len(red) == 0 || slices.ContainsFunc(red, func(name string) bool { return checks[name].Attempt <= 1 }) {
		return AutoFollowUp{}, false
	}
	return AutoFollowUp{Ticket: e.Ticket.URL, Cause: CauseCIFailed, Tip: e.LastPush.Unwrap().PushedTip}, true
}

func parkExhausted(state State, reason Reason, history AutoHistory) (State, Reason) {
	if (state != CIFailed && state != VerificationFailed) || history.Rounds < MaxAutoRounds {
		return state, reason
	}
	return Parked, Reason(fmt.Sprintf("still failing after %d automatic follow-ups: %s", history.Rounds, reason))
}

func (s Snapshot) AutoRemovals() []TicketURL {
	var out []TicketURL
	for _, e := range s.Entries {
		_, present := s.obs.Worktrees[e.Ticket.Ref()]
		_, refused := s.refusals[e.Ticket.URL]
		queued := len(s.intents[e.Ticket.URL]) + len(s.held[e.Ticket.URL])
		if e.State != PRMerged || !present || refused || queued > 0 {
			continue
		}
		out = append(out, e.Ticket.URL)
	}
	return out
}

func (s Snapshot) Undoable(url TicketURL) bool {
	e, ok := s.Entry(url)
	return ok && e.State != PRMerged
}

type Rerun struct {
	Ticket     TicketURL
	DetailsURL string
}

func (s Snapshot) Reruns() []Rerun {
	var out []Rerun
	for _, e := range s.Entries {
		pr := s.obs.PRs[e.Ticket.Ref()]
		if pr.State != Open {
			continue
		}
		for _, name := range slices.Sorted(maps.Keys(pr.Checks)) {
			c := pr.Checks[name]
			if c.Attempt == 1 && ToVerdictCheckState(c) == verdict.Failure {
				out = append(out, Rerun{Ticket: e.Ticket.URL, DetailsURL: c.DetailsURL})
			}
		}
	}
	return out
}
