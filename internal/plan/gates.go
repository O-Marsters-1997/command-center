package plan

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/O-Marsters-1997/command-center/internal/option"
)

type Intent struct {
	ID      int64
	Ticket  TicketURL
	Verb    Verb
	Payload string
}

const HeldWorktreeRefusal = "the agent is awaiting an answer; its worktree stays as it left it"

type Removal struct {
	Refusal    string
	ProvenTips []SHA
}

type ConflictAt struct {
	BranchTip SHA
	BaseTip   SHA
}

func (s Snapshot) Removal(url TicketURL) Removal {
	return s.removals[url]
}

func (s Snapshot) Session(url TicketURL) (string, bool) {
	session, ok := s.sessions[url]
	return session, ok
}

func removalFor(e Entry, pr PR) Removal {
	merged := pr.State == Merged
	var refusal string
	switch {
	case e.HoldsWorktree():
		refusal = HeldWorktreeRefusal
	case !merged && (e.Run.IsNone() || !e.Unlock.BlockerClosed):
		refusal = "neither merged nor base gone"
	}
	var tips []SHA
	if e.LastPush.IsSome() && e.LastPush.Unwrap().PushedTip != "" {
		tips = append(tips, e.LastPush.Unwrap().PushedTip)
	}
	if merged && pr.HeadOid != "" {
		tips = append(tips, pr.HeadOid)
	}
	return Removal{Refusal: refusal, ProvenTips: tips}
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

type Retarget struct {
	Ticket TicketURL
	Row    PushRow
}

type Refresh struct {
	Ticket    TicketURL
	Row       PushRow
	Requested bool
}

// Retargets lists open pull requests stacked on a base whose pull request has merged.
func (s Snapshot) Retargets() []Retarget {
	var out []Retarget
	for _, e := range s.Entries {
		if e.HoldsWorktree() || e.LastPush.IsNone() {
			continue
		}
		t, row := e.Ticket, e.LastPush.Unwrap()
		if row.BaseBranch == "" || row.BaseBranch == DefaultBaseBranch {
			continue
		}
		if s.obs.PRs[t.Ref()].State == Open &&
			s.obs.PRs[BranchRef{Repo: t.Repo, Branch: row.BaseBranch}].State == Merged {
			out = append(out, Retarget{Ticket: t.URL, Row: row})
		}
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

// Refreshes lists requested refreshes in intent order, then the automatic ones.
func (s Snapshot) Refreshes() []Refresh {
	var out []Refresh
	requested := map[TicketURL]bool{}
	for _, i := range s.Intents(VerbRefresh) {
		e, ok := s.Entry(i.Ticket)
		if !ok || requested[i.Ticket] {
			continue
		}
		requested[i.Ticket] = true
		out = append(out, Refresh{Ticket: i.Ticket, Row: e.LastPush.UnwrapOr(PushRow{}), Requested: true})
	}
	for _, e := range s.Entries {
		if requested[e.Ticket.URL] || !s.autoRefreshable(e) {
			continue
		}
		out = append(out, Refresh{Ticket: e.Ticket.URL, Row: e.LastPush.Unwrap()})
	}
	return out
}

func (s Snapshot) autoRefreshable(e Entry) bool {
	t := e.Ticket
	if e.Run.IsNone() || e.Run.Unwrap().Outcome != option.Some(OutcomePush) {
		return false
	}
	if s.obs.PRs[t.Ref()].State != Open || e.LastPush.IsNone() {
		return false
	}
	row := e.LastPush.Unwrap()
	baseTip := s.obs.BranchTips[BranchRef{Repo: t.Repo, Branch: row.BaseBranch}]
	if row.BaseBranch == "" || baseTip == row.BaseSHAAtPush {
		return false
	}
	fact := s.refreshes[t.URL]
	if !fact.Tried {
		return true
	}
	if fact.Conflict.IsNone() {
		return false
	}
	at := fact.Conflict.Unwrap()
	return s.obs.BranchTips[t.Ref()] != at.BranchTip || baseTip != at.BaseTip
}

func (s Snapshot) SkippedRepos() map[string]bool {
	skipped := map[string]bool{}
	for _, repo := range s.repos {
		if repo.Phase != RepoReady {
			skipped[repo.Name] = true
		}
	}
	for repo := range s.obs.SettingsErrors {
		skipped[repo] = true
	}
	for repo := range s.obs.RepoErrors {
		skipped[repo] = true
	}
	return skipped
}

func (s Snapshot) RepoProblems() []string {
	var parts []string
	for repo, reason := range s.obs.SettingsErrors {
		parts = append(parts, fmt.Sprintf("settings for %s: %s", repo, reason))
	}
	for repo, reason := range s.obs.RepoErrors {
		parts = append(parts, fmt.Sprintf("observe %s: %s", repo, reason))
	}
	for _, repo := range s.repos {
		if repo.Phase == RepoRefused && repo.RefusalKind != RefusalSettingsParse {
			parts = append(parts, fmt.Sprintf("%s refused (%s): %s", repo.Name, repo.RefusalKind, repo.Refusal))
		}
	}
	slices.Sort(parts)
	return parts
}
