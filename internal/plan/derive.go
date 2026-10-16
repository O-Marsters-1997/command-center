package plan

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/option"
	"github.com/O-Marsters-1997/command-center/internal/spend"
	"github.com/O-Marsters-1997/command-center/internal/verdict"
)

type LaunchMembership struct {
	LaunchID   int64
	Members    int
	Cancelled  bool
	PromptHash string
}

type RunSummary struct {
	ID            RunID
	Pgid          option.Option[int]
	ProcStartedAt option.Option[time.Time]
	Outcome       option.Option[Outcome]
	ExitCode      option.Option[int]
	EndedAt       option.Option[time.Time]
	LogPath       string
	BaselineSHA   SHA
	PromptHash    string
	Kind          RunKind
	Question      string
}

type PushRow struct {
	PushedTip     SHA
	BaseBranch    string
	BaseSHAAtPush SHA
	PushedAt      time.Time
}

type PushFact struct {
	Refused     bool
	RefusedPath string
	Failed      bool
}

type RefreshFact struct {
	Tried                    bool
	Refused                  bool
	Reason                   string
	VerificationFailed       bool
	VerificationFailedDetail string
	Conflict                 option.Option[ConflictAt]
}

type Recorded struct {
	Opened bool
	Merged bool
}

type VerdictFacts struct {
	PushRows    map[TicketURL]PushRow
	CheckingFor map[TicketURL]time.Duration
}

// Input holds the facts and observation for a derivation, keyed by ticket URL.
type Input struct {
	Now          time.Time
	Observed     bool
	Tickets      []Ticket
	Obs          Observation
	Prev         Observation
	Repos        []RepoStatus
	Memberships  map[TicketURL]LaunchMembership
	Runs         map[TicketURL]RunSummary
	Pushes       map[TicketURL]PushFact
	Refreshes    map[TicketURL]RefreshFact
	Restacked    map[TicketURL]bool
	Sessions     map[TicketURL]string
	Recorded     map[TicketURL]Recorded
	LastVerdicts map[TicketURL]string
	Verdict      VerdictFacts
	Intents      map[TicketURL][]Intent
	Removals     map[TicketURL]string
	FiveHour     float64
}

type Entry struct {
	Ticket         Ticket
	Unlock         Unlock
	State          State
	Reason         Reason
	Run            option.Option[RunFact]
	Elapsed        option.Option[time.Duration]
	LogPath        string
	ConflictedBase string
	DraftReason    string
	LaunchID       int64
	Base           string
	ReadyToUndraft bool
	OpensAsDraft   bool
	PromptHash     string
	LastPush       option.Option[PushRow]
}

func (e Entry) HoldsWorktree() bool {
	return e.Run.IsSome() && !e.Run.Unwrap().Alive && e.Run.Unwrap().Outcome == option.Some(OutcomeAsked)
}

type Snapshot struct {
	Entries     []Entry
	byURL       map[TicketURL]int
	launch      []LaunchCandidate
	running     int
	maxAgents   int
	spendPaused bool
	intents     map[TicketURL][]Intent
	transitions []Transition
	verdicts    map[TicketURL]string
}

func (s Snapshot) LaunchAfter(spawned int) []TicketURL {
	return LaunchPlan(s.launch, s.running+spawned, s.maxAgents, s.spendPaused)
}

func (s Snapshot) Skipping(repos map[string]bool) Snapshot {
	if len(repos) == 0 {
		return s
	}
	out := s
	out.Entries = nil
	out.launch = nil
	out.byURL = make(map[TicketURL]int, len(s.Entries))
	for _, e := range s.Entries {
		if repos[e.Ticket.Repo] {
			continue
		}
		out.byURL[e.Ticket.URL] = len(out.Entries)
		out.Entries = append(out.Entries, e)
	}
	for _, c := range s.launch {
		if _, ok := out.byURL[c.URL]; ok {
			out.launch = append(out.launch, c)
		}
	}
	return out
}

func (s Snapshot) Entry(url TicketURL) (Entry, bool) {
	i, ok := s.byURL[url]
	if !ok {
		return Entry{}, false
	}
	return s.Entries[i], true
}

func (s Snapshot) Offers(url TicketURL, verb Verb) bool {
	e, ok := s.Entry(url)
	return ok && slices.Contains(Verbs(e.State), verb)
}

func ticketsByURL(tickets []Ticket) map[TicketURL]Ticket {
	byURL := make(map[TicketURL]Ticket, len(tickets))
	for _, t := range tickets {
		byURL[t.URL] = t
	}
	return byURL
}

func prsByBranch(tickets []Ticket, obs Observation) map[string]PRState {
	prs := make(map[string]PRState, len(tickets))
	for _, t := range tickets {
		prs[t.Branch] = obs.PRs[t.Ref()].State
	}
	return prs
}

func (r Rules) Derive(in Input) Snapshot {
	byURL := ticketsByURL(in.Tickets)
	prs := prsByBranch(in.Tickets, in.Obs)
	peers := r.conflictingPeerHold(in.Tickets, byURL, prs, in.Obs)

	snap := Snapshot{
		Entries:     make([]Entry, 0, len(in.Tickets)),
		byURL:       make(map[TicketURL]int, len(in.Tickets)),
		maxAgents:   r.MaxAgents,
		spendPaused: spend.Paused(in.FiveHour, r.SpendLimit5h),
		intents:     in.Intents,
	}
	for _, t := range in.Tickets {
		unlock := Unlocked(t, byURL, prs, r.Stacking[t.Repo])
		base := unlock.BaseBranch
		if base == "" {
			base = ProspectiveBase(t, byURL, r.Stacking[t.Repo])
		}
		run, elapsed, logPath := r.runFor(t, in, peers[t.URL])
		membership := in.Memberships[t.URL]
		authorised := membership.LaunchID != 0
		var conflictedBase string
		if r.conflictedBase(t, base, in) {
			conflictedBase = base
		}
		state, reason := Status(Facts{
			Unlock:          unlock,
			Authorised:      authorised,
			LatestRun:       run,
			CancelledMember: membership.Cancelled,
			ConflictedBase:  conflictedBase,
		})
		snap.launch = append(snap.launch, LaunchCandidate{
			URL:               t.URL,
			Unlock:            unlock,
			Authorised:        authorised,
			PromptHashMatches: authorised && membership.PromptHash == Hash(Compose(t)),
			HasRun:            run.IsSome(),
			ConflictedBase:    conflictedBase,
		})
		if in.Runs[t.URL].Pgid.IsSome() && run.IsSome() && run.Unwrap().Outcome.IsNone() {
			snap.running++
		}

		pr := in.Obs.PRs[t.Ref()]
		gating := GatingBlockers(t, byURL)
		draftWhy, readyToUndraft := draftReason(pr, gating, prs, run)
		lastPush := option.None[PushRow]()
		if row, ok := in.Verdict.PushRows[t.URL]; ok {
			lastPush = option.Some(row)
		}
		snap.byURL[t.URL] = len(snap.Entries)
		snap.Entries = append(snap.Entries, Entry{
			Ticket: t, Unlock: unlock, State: state, Reason: reason, Run: run,
			Elapsed: elapsed, LogPath: logPath, ConflictedBase: conflictedBase,
			DraftReason: draftWhy, ReadyToUndraft: readyToUndraft && pr.State == Open,
			OpensAsDraft: len(gating) > 0,
			PromptHash:   membership.PromptHash, LastPush: lastPush,
			LaunchID: membership.LaunchID, Base: base,
		})
	}
	snap.transitions, snap.verdicts = r.transitions(in)
	return snap
}

func (r Rules) conflictedBase(t Ticket, base string, in Input) bool {
	if base == DefaultBaseBranch {
		return false
	}
	baseRef := BranchRef{Repo: t.Repo, Branch: base}
	return in.Obs.ConflictsWithBase[baseRef] || in.Obs.MidMerge[baseRef]
}

func (r Rules) conflictingPeerHold(
	tickets []Ticket, byURL map[TicketURL]Ticket, prs map[string]PRState, obs Observation,
) map[TicketURL]string {
	var candidates []Ticket
	for _, t := range tickets {
		if prs[t.Branch] != Open {
			continue
		}
		if ProspectiveBase(byURL[t.URL], byURL, r.Stacking[t.Repo]) != DefaultBaseBranch {
			continue
		}
		candidates = append(candidates, t)
	}
	slices.SortFunc(candidates, func(a, b Ticket) int { return compareByRef(a.Branch, b.Branch) })

	held := make(map[TicketURL]string, len(candidates))
	for i, t := range candidates {
		for _, peer := range candidates[:i] {
			if _, peerHeld := held[peer.URL]; peerHeld {
				continue
			}
			if obs.ConflictsWithPeer[t.Ref()][peer.Ref()] {
				held[t.URL] = peer.Branch
				break
			}
		}
	}
	return held
}

func compareByRef(a, b string) int {
	na, oka := branchNumber(a)
	nb, okb := branchNumber(b)
	if oka && okb {
		return cmp.Compare(na, nb)
	}
	return strings.Compare(a, b)
}

func branchNumber(branch string) (int, bool) {
	rest, ok := strings.CutPrefix(branch, "cc-")
	if !ok {
		return 0, false
	}
	numStr, _, _ := strings.Cut(rest, "-")
	n, err := strconv.Atoi(numStr)
	return n, err == nil
}

func draftReason(
	pr PR, gating []Ticket, prs map[string]PRState, run option.Option[RunFact],
) (reason string, ready bool) {
	if !pr.IsDraft {
		return "", false
	}
	reviewMe := run.IsSome() && run.Unwrap().Verdict.IsSome() && run.Unwrap().Verdict.Unwrap().Kind == verdict.ReviewMe
	draft, gateReason := DraftGate(gating, prs, reviewMe)
	if !draft {
		return "ready to un-draft; the last gh pr ready call has not taken effect yet", true
	}
	return string(gateReason), false
}

func (r Rules) runFor(
	t Ticket, in Input, peer string,
) (run option.Option[RunFact], elapsed option.Option[time.Duration], logPath string) {
	summary, ok := in.Runs[t.URL]
	if !ok {
		return option.None[RunFact](), option.None[time.Duration](), ""
	}

	key := t.Ref()
	fact := RunFact{LogPath: summary.LogPath, Alive: in.Obs.Runs[t.URL].Alive}
	ownState := in.Obs.PRs[key].State
	fact.PROpen = ownState == Open
	fact.PRMerged = ownState == Merged
	fact.PRClosedUnmerged = ownState == Closed
	if summary.Outcome.IsSome() {
		fact.Outcome = summary.Outcome
		fact.Question = summary.Question
		if summary.Outcome.Unwrap() == OutcomePush {
			pf := in.Pushes[t.URL]
			fact.PushRefused = pf.Refused
			fact.PushRefusedPath = pf.RefusedPath
			fact.PushFailed = pf.Failed
			rf := in.Refreshes[t.URL]
			fact.RefreshRefused = rf.Refused
			fact.RefreshRefusedReason = Reason(rf.Reason)
			fact.VerificationFailed = rf.VerificationFailed
			fact.VerificationFailedReason = Reason(rf.VerificationFailedDetail)
			fact.MidMerge = in.Obs.MidMerge[key]
			if in.Obs.ConflictsWithBase[key] {
				fact.ConflictsWithMain = true
				fact.ConflictsWithMainReason = Reason(fmt.Sprintf("%s no longer merges cleanly into main", t.Branch))
			}
			fact.ConflictingPeer = peer
			if fact.PROpen && !fact.PushRefused && !fact.PushFailed {
				fact.Verdict = r.ApplyVerdict(t, in.Obs, in.Verdict)
			}
		}
	}

	elapsed = option.None[time.Duration]()
	if started := summary.ProcStartedAt; fact.Alive && started.IsSome() {
		elapsed = option.Some(in.Now.Sub(started.Unwrap()).Round(time.Second))
	}
	return option.Some(fact), elapsed, summary.LogPath
}

func (r Rules) ApplyVerdict(t Ticket, obs Observation, vf VerdictFacts) option.Option[verdict.Result] {
	predicate := r.Checks[t.Repo]
	if predicate.IsZero() {
		return option.None[verdict.Result]()
	}
	pushRow, pushed := vf.PushRows[t.URL]
	if !pushed {
		return option.None[verdict.Result]()
	}

	pr := obs.PRs[t.Ref()]
	mergifySHA := r.MergifySHA[t.Repo]

	result := verdict.Evaluate(predicate, verdict.Input{
		Checks:       verdictChecks(pr.Checks),
		HeadOidMatch: pushRow.PushedTip != "" && pr.HeadOid == pushRow.PushedTip,
		StackedBase:  pushRow.BaseBranch != "",
		BaseSHAMatch: obs.BranchTips[BranchRef{Repo: t.Repo, Branch: pushRow.BaseBranch}] == pushRow.BaseSHAAtPush,
		ConfigHashOK: mergifySHA == "" || obs.MergifyHash[t.Repo] == mergifySHA,
		PushedAt:     pushRow.PushedAt,
		Now:          pushRow.PushedAt.Add(vf.CheckingFor[t.URL]),
		AuthorLogin:  pr.AuthorLogin,
		CompatCheck:  r.CompatCheck[t.Repo],
	})

	return option.Some(result)
}

func verdictChecks(checks map[string]CheckState) map[string]verdict.CheckState {
	out := make(map[string]verdict.CheckState, len(checks))
	for name, c := range checks {
		out[name] = ToVerdictCheckState(c)
	}
	return out
}

// ToVerdictCheckState maps a completed check other than SUCCESS or SKIPPED to Failure.
func ToVerdictCheckState(cs CheckState) verdict.CheckState {
	if cs.Status != "COMPLETED" {
		return verdict.Pending
	}
	switch cs.Conclusion {
	case "SUCCESS":
		return verdict.Success
	case "SKIPPED":
		return verdict.Skipped
	default:
		return verdict.Failure
	}
}
