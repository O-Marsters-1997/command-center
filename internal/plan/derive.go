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

// BranchKey is the repo-qualified key every branch-keyed Observation map must use.
func BranchKey(repo, branch string) string { return repo + "//" + branch }

type LaunchMembership struct {
	LaunchID   int64
	Members    int
	Cancelled  bool
	PromptHash string
}

// RunSummary is the latest run the board and the launch-eligibility check need per ticket.
type RunSummary struct {
	ID            int64
	Pgid          option.Option[int]
	ProcStartedAt option.Option[time.Time]
	Outcome       option.Option[Outcome]
	ExitCode      option.Option[int]
	EndedAt       option.Option[time.Time]
	LogPath       string
	BaselineSHA   string
	PromptHash    string
	Kind          RunKind
	Question      string
}

type PushRow struct {
	PushedTip     string
	BaseBranch    string
	BaseSHAAtPush string
	PushedAt      time.Time
}

type PushFact struct {
	Refused     bool
	RefusedPath string
	Failed      bool
}

type RefreshFact struct {
	Refused                  bool
	Reason                   string
	VerificationFailed       bool
	VerificationFailedDetail string
}

type VerdictFacts struct {
	PushRows    map[string]PushRow
	CheckingFor map[string]time.Duration
}

// Input is every durable fact and the observation one derivation reads, keyed by ticket URL.
// Now is passed in because this package never calls time.Now.
type Input struct {
	Now          time.Time
	Observed     bool
	Tickets      []Ticket
	Obs          Observation
	Memberships  map[string]LaunchMembership
	Runs         map[string]RunSummary
	Pushes       map[string]PushFact
	Refreshes    map[string]RefreshFact
	Verdict      VerdictFacts
	PendingVerbs map[string][]Verb
	Removals     map[string]string
	FiveHour     float64
}

// Entry is one ticket's derived state: what the board shows and the verbs it may offer.
// Run is None until the ticket's first run; Elapsed is None unless that run is alive.
type Entry struct {
	Ticket         Ticket
	Unlock         Unlock
	State          State
	Reason         Reason
	Run            option.Option[RunFact]
	Pgid           option.Option[int]
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
	byURL       map[string]int
	launch      []LaunchCandidate
	running     int
	maxAgents   int
	spendPaused bool
}

func (s Snapshot) LaunchAfter(spawned int) []string {
	return LaunchPlan(s.launch, s.running+spawned, s.maxAgents, s.spendPaused)
}

// HasSlotAfter reports whether a run may spawn when spawned agents have started since this snapshot.
func (s Snapshot) HasSlotAfter(spawned int) bool {
	return !s.spendPaused && s.maxAgents-s.running-spawned > 0
}

// Skipping is the snapshot without the tickets of the named repos: nothing is acted on for a repo
// whose settings could not be read this tick.
func (s Snapshot) Skipping(repos map[string]bool) Snapshot {
	if len(repos) == 0 {
		return s
	}
	out := s
	out.Entries = nil
	out.launch = nil
	out.byURL = make(map[string]int, len(s.Entries))
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

func (s Snapshot) Entry(url string) (Entry, bool) {
	i, ok := s.byURL[url]
	if !ok {
		return Entry{}, false
	}
	return s.Entries[i], true
}

func (s Snapshot) Offers(url string, verb Verb) bool {
	e, ok := s.Entry(url)
	return ok && slices.Contains(Verbs(e.State), verb)
}

func ticketsByURL(tickets []Ticket) map[string]Ticket {
	byURL := make(map[string]Ticket, len(tickets))
	for _, t := range tickets {
		byURL[t.URL] = t
	}
	return byURL
}

func prsByBranch(tickets []Ticket, obs Observation) map[string]PRState {
	prs := make(map[string]PRState, len(tickets))
	for _, t := range tickets {
		prs[t.Branch] = obs.PRs[BranchKey(t.Repo, t.Branch)].State
	}
	return prs
}

// Derive labels every ticket from the stored facts plus this tick's observation. No status is
// stored: labels are derived on every call.
func (r Rules) Derive(in Input) Snapshot {
	byURL := ticketsByURL(in.Tickets)
	prs := prsByBranch(in.Tickets, in.Obs)
	peers := r.conflictingPeerHold(in.Tickets, byURL, prs, in.Obs)

	snap := Snapshot{
		Entries:     make([]Entry, 0, len(in.Tickets)),
		byURL:       make(map[string]int, len(in.Tickets)),
		maxAgents:   r.MaxAgents,
		spendPaused: spend.Paused(in.FiveHour, r.SpendLimit5h),
	}
	for _, t := range in.Tickets {
		unlock := Unlocked(t, byURL, prs, r.Stacking[t.Repo])
		base := unlock.BaseBranch
		if base == "" {
			base = ProspectiveBase(t, byURL, r.Stacking[t.Repo])
		}
		run, pgid, elapsed, logPath := r.runFor(t, in, peers[t.URL])
		membership := in.Memberships[t.URL]
		authorised := membership.LaunchID != 0
		conflictedBase := r.ConflictedBase(t, byURL, unlock, in.Obs)
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
		if pgid.IsSome() && run.IsSome() && run.Unwrap().Outcome.IsNone() {
			snap.running++
		}

		pr := in.Obs.PRs[BranchKey(t.Repo, t.Branch)]
		draftWhy, readyToUndraft := draftReason(pr, t, byURL, prs, run)
		lastPush := option.None[PushRow]()
		if row, ok := in.Verdict.PushRows[t.URL]; ok {
			lastPush = option.Some(row)
		}
		snap.byURL[t.URL] = len(snap.Entries)
		snap.Entries = append(snap.Entries, Entry{
			Ticket: t, Unlock: unlock, State: state, Reason: reason, Run: run,
			Pgid: pgid, Elapsed: elapsed, LogPath: logPath, ConflictedBase: conflictedBase,
			DraftReason: draftWhy, ReadyToUndraft: readyToUndraft && pr.State == Open,
			OpensAsDraft: len(GatingBlockers(t, byURL)) > 0,
			PromptHash:   membership.PromptHash, LastPush: lastPush,
			LaunchID: membership.LaunchID, Base: base,
		})
	}
	return snap
}

// ConflictedBase names the base a launch would cut this ticket from when that base already
// carries a merge conflict, and "" when it is clean. A locked row is judged on the base it
// would get once unlocked.
func (r Rules) ConflictedBase(t Ticket, byURL map[string]Ticket, unlock Unlock, obs Observation) string {
	base := unlock.BaseBranch
	if base == "" {
		base = ProspectiveBase(t, byURL, r.Stacking[t.Repo])
	}
	if base == DefaultBaseBranch {
		return ""
	}
	if obs.ConflictsWithBase[BranchKey(t.Repo, base)] || obs.MidMerge[BranchKey(t.Repo, base)] {
		return base
	}
	return ""
}

func (r Rules) conflictingPeerHold(
	tickets []Ticket, byURL map[string]Ticket, prs map[string]PRState, obs Observation,
) map[string]string {
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

	held := make(map[string]string, len(candidates))
	for i, t := range candidates {
		for _, peer := range candidates[:i] {
			if _, peerHeld := held[peer.URL]; peerHeld {
				continue
			}
			if obs.ConflictsWithPeer[BranchKey(t.Repo, t.Branch)][BranchKey(peer.Repo, peer.Branch)] {
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
	pr PR, t Ticket, byURL map[string]Ticket, prs map[string]PRState, run option.Option[RunFact],
) (reason string, ready bool) {
	if !pr.IsDraft {
		return "", false
	}
	gating := GatingBlockers(t, byURL)
	reviewMe := run.IsSome() && run.Unwrap().Verdict.IsSome() && run.Unwrap().Verdict.Unwrap().Kind == verdict.ReviewMe
	draft, gateReason := DraftGate(gating, prs, reviewMe)
	if !draft {
		return "ready to un-draft; the last gh pr ready call has not taken effect yet", true
	}
	return string(gateReason), false
}

func (r Rules) runFor(
	t Ticket, in Input, peer string,
) (run option.Option[RunFact], pgid option.Option[int], elapsed option.Option[time.Duration], logPath string) {
	summary, ok := in.Runs[t.URL]
	if !ok {
		return option.None[RunFact](), option.None[int](), option.None[time.Duration](), ""
	}

	key := BranchKey(t.Repo, t.Branch)
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
	return option.Some(fact), summary.Pgid, elapsed, summary.LogPath
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

	pr := obs.PRs[BranchKey(t.Repo, t.Branch)]
	mergifySHA := r.MergifySHA[t.Repo]

	result := verdict.Evaluate(predicate, verdict.Input{
		Checks:       verdictChecks(pr.Checks),
		HeadOidMatch: pushRow.PushedTip != "" && pr.HeadOid == pushRow.PushedTip,
		StackedBase:  pushRow.BaseBranch != "",
		BaseSHAMatch: obs.BranchTips[BranchKey(t.Repo, pushRow.BaseBranch)] == pushRow.BaseSHAAtPush,
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

// ToVerdictCheckState reads anything completed but not exactly SUCCESS or SKIPPED as a definite
// Failure, never a third kind of maybe.
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
