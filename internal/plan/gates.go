package plan

import (
	"cmp"
	"fmt"
	"slices"
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
