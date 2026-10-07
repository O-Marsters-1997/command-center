package plan

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/spend"
	"github.com/O-Marsters-1997/command-center/internal/verdict"
)

const RunKindResolve = "resolve"

func BranchKey(repo, branch string) string { return repo + "//" + branch }

type LaunchMembership struct {
	LaunchID   int64
	Members    int
	Cancelled  bool
	PromptHash string
}

// RunSummary is the latest run the board and the launch-eligibility check need per ticket.
// Kind is "agent" for a launch or re-run and RunKindResolve for a conflict resolution.
type RunSummary struct {
	ID            int64
	Pgid          *int
	ProcStartedAt *time.Time
	HasOutcome    bool
	Outcome       Outcome
	ExitCode      *int
	EndedAt       *time.Time
	LogPath       string
	BaselineSHA   string
	PromptHash    string
	Kind          string
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
	PendingVerbs map[string][]string
	Removals     map[string]string
	FiveHour     float64
}

// Entry is one ticket's derived state: what the board shows and the verbs it may offer.
// Run is nil until the ticket's first run; Elapsed is nil unless that run is alive.
type Entry struct {
	Ticket         Ticket
	Unlock         Unlock
	State          State
	Reason         Reason
	Run            *RunFact
	Pgid           *int
	Elapsed        *time.Duration
	LogPath        string
	ConflictedBase string
	DraftReason    string
	LaunchID       int64
	Base           string
	ReadyToUndraft bool
	OpensAsDraft   bool
	PromptHash     string
	LastPush       *PushRow
}

type Snapshot struct {
	Entries     []Entry
	byURL       map[string]int
	launch      []LaunchCandidate
	running     int
	maxAgents   int
	spendPaused bool
}

// Launch is the ticket URLs to cut and spawn: every eligible candidate in input order, capped at
// the free agent slots, and none while the five-hour reading is at spend_limit_5h.
func (s Snapshot) Launch() []string { return s.LaunchAfter(0) }

// LaunchAfter is Launch for a tick that has since spawned agents, so the snapshot's running
// count is stale by that much.
func (s Snapshot) LaunchAfter(spawned int) []string {
	return LaunchPlan(s.launch, s.running+spawned, s.maxAgents, s.spendPaused)
}

func (s Snapshot) Entry(url string) (Entry, bool) {
	i, ok := s.byURL[url]
	if !ok {
		return Entry{}, false
	}
	return s.Entries[i], true
}

func (s Snapshot) Offers(url, verb string) bool {
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
			HasRun:            run != nil,
			ConflictedBase:    conflictedBase,
		})
		if pgid != nil && !run.HasOutcome {
			snap.running++
		}

		pr := in.Obs.PRs[BranchKey(t.Repo, t.Branch)]
		draftWhy, readyToUndraft := draftReason(pr, t, byURL, prs, run)
		var lastPush *PushRow
		if row, ok := in.Verdict.PushRows[t.URL]; ok {
			lastPush = &row
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
	if base == defaultBranch {
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
		if ProspectiveBase(byURL[t.URL], byURL, r.Stacking[t.Repo]) != defaultBranch {
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
	pr PR, t Ticket, byURL map[string]Ticket, prs map[string]PRState, run *RunFact,
) (reason string, ready bool) {
	if !pr.IsDraft {
		return "", false
	}
	gating := GatingBlockers(t, byURL)
	draft, gateReason := DraftGate(gating, prs, run != nil && run.VerdictReviewMe)
	if !draft {
		return "ready to un-draft; the last gh pr ready call has not taken effect yet", true
	}
	return string(gateReason), false
}

func (r Rules) runFor(
	t Ticket, in Input, peer string,
) (run *RunFact, pgid *int, elapsed *time.Duration, logPath string) {
	summary, ok := in.Runs[t.URL]
	if !ok {
		return nil, nil, nil, ""
	}

	key := BranchKey(t.Repo, t.Branch)
	fact := &RunFact{LogPath: summary.LogPath, Alive: in.Obs.Runs[t.URL].Alive}
	ownState := in.Obs.PRs[key].State
	fact.PROpen = ownState == Open
	fact.PRMerged = ownState == Merged
	fact.PRClosedUnmerged = ownState == Closed
	if summary.HasOutcome {
		fact.HasOutcome = true
		fact.Outcome = summary.Outcome
		if summary.Outcome == OutcomePush {
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
				r.ApplyVerdict(fact, t, in.Obs, in.Verdict)
			}
		}
		if summary.Outcome == OutcomeFailed && summary.Kind == RunKindResolve && in.Obs.MidMerge[key] {
			fact.Resolved = true
		}
	}

	if fact.Alive && summary.ProcStartedAt != nil {
		d := in.Now.Sub(*summary.ProcStartedAt).Round(time.Second)
		elapsed = &d
	}
	return fact, summary.Pgid, elapsed, summary.LogPath
}

func (r Rules) ApplyVerdict(fact *RunFact, t Ticket, obs Observation, vf VerdictFacts) {
	predicate := r.Checks[t.Repo]
	if predicate.IsZero() {
		return
	}
	pushRow, pushed := vf.PushRows[t.URL]
	if !pushed {
		return
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

	switch result.Verdict {
	case verdict.ReviewMe:
		fact.VerdictReviewMe = true
	case verdict.WaitingOnProducerDeploy:
		fact.VerdictWaitingOnProducer = true
	case verdict.NeedsYou:
		if len(result.RedLeaves) > 0 {
			fact.VerdictCIFailed = true
			fact.RedLeaves = result.RedLeaves
		} else {
			fact.VerdictNeedsYou = true
		}
	case verdict.BaseMoved:
		fact.VerdictBaseMoved = true
	case verdict.Checking:
	}
	fact.VerdictReason = Reason(result.Reason)
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
