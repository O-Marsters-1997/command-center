package plan

import (
	"cmp"
	"slices"

	"github.com/O-Marsters-1997/command-center/internal/option"
)

type Intent struct {
	ID      int64
	Ticket  TicketURL
	Verb    Verb
	Payload string
}

type ConflictAt struct {
	BranchTip SHA
	BaseTip   SHA
}

func (s Snapshot) Intents(verb Verb) []Intent {
	var out []Intent
	for _, intents := range s.intents {
		for _, i := range intents {
			if i.Verb == verb {
				out = append(out, i)
			}
		}
	}
	slices.SortFunc(out, func(a, b Intent) int { return cmp.Compare(a.ID, b.ID) })
	return out
}

type Push struct {
	Ticket       TicketURL
	LocalTip     SHA
	Force        bool
	OpensAsDraft bool
	Body         string
}

func (s Snapshot) Pushable(obs Observation) []Push {
	var out []Push
	for _, e := range s.Entries {
		if e.Run.IsNone() || e.Run.Unwrap().Outcome != option.Some(OutcomePush) || obs.Runs[e.Ticket.URL].Alive {
			continue
		}
		run := e.Run.Unwrap()
		if run.PushFailed || run.PushRefused || run.VerificationFailed {
			continue
		}
		p, ok := s.Push(e.Ticket.URL, obs)
		if !ok || (e.LastPush.IsSome() && e.LastPush.Unwrap().PushedTip == p.LocalTip) {
			continue
		}
		out = append(out, p)
	}
	return out
}

// Push builds the push of an unlocked ticket that has a local tip, ignoring
// the gates Pushable applies; a manual retry uses it.
func (s Snapshot) Push(url TicketURL, obs Observation) (Push, bool) {
	e, ok := s.Entry(url)
	if !ok || !e.Unlock.Unlocked {
		return Push{}, false
	}
	tip := obs.LocalTips[e.Ticket.Ref()]
	if tip == "" {
		return Push{}, false
	}
	base := BranchRef{Repo: e.Ticket.Repo, Branch: e.Unlock.BaseBranch}
	return Push{
		Ticket: url, LocalTip: tip, Force: e.Restacked, OpensAsDraft: e.OpensAsDraft,
		Body: PRBody(base.Branch, obs.PRs[base].Number),
	}, true
}
