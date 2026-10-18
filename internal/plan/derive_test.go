package plan_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	"github.com/O-Marsters-1997/command-center/internal/plan/verdict"
)

func key(branch string) plan.BranchRef { return plan.BranchRef{Repo: "r", Branch: branch} }

func ticket(n, branch string, blockedBy ...plan.TicketURL) plan.Ticket {
	return plan.Ticket{URL: plan.TicketURL("sandbox://CC-" + n), Repo: "r", Branch: branch, BlockedBy: blockedBy}
}

func openPRs(tickets ...plan.Ticket) map[plan.BranchRef]plan.PR {
	prs := make(map[plan.BranchRef]plan.PR, len(tickets))
	for _, t := range tickets {
		prs[key(t.Branch)] = plan.PR{Number: 1, State: plan.Open}
	}
	return prs
}

func derive(rules plan.Rules, in plan.Input) plan.Snapshot {
	if in.Now.IsZero() {
		in.Now = time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	}
	return rules.Derive(in)
}

func entry(t *testing.T, snap plan.Snapshot, url plan.TicketURL) plan.Entry {
	t.Helper()
	e, ok := snap.Entry(url)
	if !ok {
		t.Fatalf("snapshot has no entry for %s", url)
	}
	return e
}

func TestRules_Derive(t *testing.T) {
	t.Parallel()

	t.Run("conflicted base", deriveConflictedBase)
	t.Run("conflicting peer", deriveConflictingPeer)
	t.Run("a chain does not serialise past the holding peer", deriveChainHeldByOnePeer)
	t.Run("draft reason", deriveDraftReason)
	t.Run("workflow pass", deriveWorkflowPass)
}

func deriveWorkflowPass(t *testing.T) {
	t.Parallel()

	steps := []plan.Step{{Name: "read"}, {Name: "check"}, {Name: "fix", OnFail: true}, {Name: "pr"}}
	v1 := plan.Workflow{Name: "v1", Hash: "h1", MaxFixRounds: 1, Steps: steps}
	v0 := plan.Workflow{Name: "v0", Hash: "h0", Steps: []plan.Step{{Name: "implement"}}}
	rules := plan.Rules{Workflows: map[string]plan.Workflow{"v0": v0, "v1": v1}}
	tk := ticket("1", "cc-1-first")
	passed, failed := option.Some(plan.OutcomePassed), option.Some(plan.OutcomeFailed)
	run := func(step plan.StepName, o option.Option[plan.Outcome]) plan.StepRun {
		return plan.StepRun{ID: 1, Step: step, Outcome: o}
	}
	derived := func(hash string, last plan.StepRun, pass ...plan.StepRun) plan.Entry {
		in := plan.Input{
			Tickets:     []plan.Ticket{tk},
			Memberships: map[plan.TicketURL]plan.LaunchMembership{tk.URL: {LaunchID: 1, Workflow: "v1"}},
			Runs: map[plan.TicketURL]plan.RunSummary{
				tk.URL: {Outcome: last.Outcome, Step: last.Step, WorkflowHash: hash},
			},
			Passes: map[plan.TicketURL][]plan.StepRun{tk.URL: pass},
		}
		return entry(t, derive(rules, in), tk.URL)
	}

	tests := []struct {
		name      string
		hash      string
		last      plan.StepRun
		pass      []plan.StepRun
		state     plan.State
		reason    string
		wantFlown bool
	}{
		{
			name: "a passed step queues the next", hash: "h1", last: run("read", passed),
			pass: []plan.StepRun{run("read", passed)}, state: plan.Queued, reason: "next step: check", wantFlown: true,
		},
		{
			name: "a failed check queues fix", hash: "h1", last: run("check", failed),
			pass:  []plan.StepRun{run("read", passed), run("check", failed)},
			state: plan.Queued, reason: "next step: fix (fix round 1)", wantFlown: true,
		},
		{
			name: "exhausted fix rounds fail naming the step", hash: "h1", last: run("check", failed),
			pass: []plan.StepRun{
				run("read", passed), run("check", failed), run("fix", passed), run("check", failed),
			},
			state: plan.Failed, reason: "step check",
		},
		{
			name: "a pass whose workflow left the build fails", hash: "gone", last: run("read", passed),
			pass: []plan.StepRun{run("read", passed)}, state: plan.Failed, reason: "not in this build",
		},
		{
			name: "a failed single-step workflow keeps its own reason", hash: "h0", last: run("implement", failed),
			pass: []plan.StepRun{run("implement", failed)}, state: plan.Failed, reason: "no commits after",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := derived(tt.hash, tt.last, tt.pass...)
			if got.State != tt.state || !strings.Contains(string(got.Reason), tt.reason) {
				t.Errorf("state, reason = %v, %q; want %v containing %q", got.State, got.Reason, tt.state, tt.reason)
			}
			if inFlight := got.Next.Kind == plan.NextRun && got.Next.Step.IsSome(); inFlight != tt.wantFlown {
				t.Errorf("Next runs a step = %t, want %t", inFlight, tt.wantFlown)
			}
		})
	}
}

func deriveConflictedBase(t *testing.T) {
	t.Parallel()

	root := ticket("1", "cc-1-first")
	child := ticket("2", "cc-2-second", root.URL)
	tickets := []plan.Ticket{root, child}

	conflicts := plan.Observation{ConflictsWithBase: map[plan.BranchRef]bool{key(root.Branch): true}}
	midMerge := plan.Observation{MidMerge: map[plan.BranchRef]bool{key(root.Branch): true}}
	clean := plan.Observation{ConflictsWithBase: map[plan.BranchRef]bool{key(root.Branch): false}}
	withPR := func(o plan.Observation) plan.Observation {
		o.PRs = openPRs(root)
		return o
	}

	tests := []struct {
		name     string
		stacking bool
		obs      plan.Observation
		url      plan.TicketURL
		want     string
	}{
		{
			name: "a base that does not merge cleanly", stacking: true,
			obs: withPR(conflicts), url: child.URL, want: root.Branch,
		},
		{name: "a base left mid-merge", stacking: true, obs: withPR(midMerge), url: child.URL, want: root.Branch},
		{name: "a clean base", stacking: true, obs: withPR(clean), url: child.URL, want: ""},
		{name: "main is never the conflicted base", stacking: true, obs: withPR(conflicts), url: root.URL, want: ""},
		{
			name:     "a locked row is judged on the base it would get once unlocked",
			stacking: true, obs: conflicts, url: child.URL, want: root.Branch,
		},
		{name: "an unstacked cut is from main", stacking: false, obs: withPR(conflicts), url: child.URL, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rules := plan.Rules{Stacking: map[string]bool{"r": tt.stacking}}
			snap := derive(rules, plan.Input{Tickets: tickets, Obs: tt.obs})
			if got := entry(t, snap, tt.url).ConflictedBase; got != tt.want {
				t.Errorf("Derive().Entry(%s).ConflictedBase = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func pushedRun() plan.RunSummary {
	return plan.RunSummary{Outcome: option.Some(plan.OutcomePush), LogPath: "run.log"}
}

func peerConflicts(pairs ...[2]string) map[plan.BranchRef]map[plan.BranchRef]bool {
	out := map[plan.BranchRef]map[plan.BranchRef]bool{}
	for _, p := range pairs {
		if out[key(p[0])] == nil {
			out[key(p[0])] = map[plan.BranchRef]bool{}
		}
		out[key(p[0])][key(p[1])] = true
	}
	return out
}

func deriveConflictingPeer(t *testing.T) {
	t.Parallel()

	lower := ticket("1", "cc-1-lower")
	higher := ticket("2", "cc-2-higher")
	both := peerConflicts([2]string{lower.Branch, higher.Branch}, [2]string{higher.Branch, lower.Branch})

	tests := []struct {
		name     string
		tickets  []plan.Ticket
		stacking bool
		prs      map[plan.BranchRef]plan.PR
		peers    map[plan.BranchRef]map[plan.BranchRef]bool
		want     map[plan.TicketURL]string
	}{
		{
			name:    "a conflicting pair holds the higher ref behind the lower",
			tickets: []plan.Ticket{higher, lower}, prs: openPRs(lower, higher), peers: both,
			want: map[plan.TicketURL]string{higher.URL: lower.Branch},
		},
		{
			name:    "a clean pair holds neither",
			tickets: []plan.Ticket{higher, lower}, prs: openPRs(lower, higher),
			peers: map[plan.BranchRef]map[plan.BranchRef]bool{key(lower.Branch): {key(higher.Branch): false}},
			want:  map[plan.TicketURL]string{},
		},
		{
			name:     "a stacked branch is never a candidate",
			tickets:  []plan.Ticket{lower, ticket("2", higher.Branch, lower.URL)},
			stacking: true, prs: openPRs(lower, higher), peers: both,
			want: map[plan.TicketURL]string{},
		},
		{
			name:    "a branch with no open pull request is never a candidate",
			tickets: []plan.Ticket{higher, lower}, prs: openPRs(lower), peers: both,
			want: map[plan.TicketURL]string{},
		},
		{
			name: "ref order is by ticket number, not branch string, across a digit-count boundary",
			tickets: []plan.Ticket{
				ticket("100", "cc-100-hundred"), ticket("9", "cc-9-nine"),
			},
			prs: openPRs(ticket("100", "cc-100-hundred"), ticket("9", "cc-9-nine")),
			peers: peerConflicts(
				[2]string{"cc-9-nine", "cc-100-hundred"}, [2]string{"cc-100-hundred", "cc-9-nine"}),
			want: map[plan.TicketURL]string{"sandbox://CC-100": "cc-9-nine"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			in := plan.Input{
				Tickets: tt.tickets,
				Obs:     plan.Observation{PRs: tt.prs, ConflictsWithPeer: tt.peers},
				Runs:    map[plan.TicketURL]plan.RunSummary{},
			}
			for _, tk := range tt.tickets {
				in.Runs[tk.URL] = pushedRun()
			}
			snap := derive(plan.Rules{Stacking: map[string]bool{"r": tt.stacking}}, in)

			got := map[plan.TicketURL]string{}
			for _, tk := range tt.tickets {
				if e := entry(t, snap, tk.URL); e.Run.IsSome() && e.Run.Unwrap().ConflictingPeer != "" {
					got[tk.URL] = e.Run.Unwrap().ConflictingPeer
				}
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Derive() ConflictingPeer by ticket mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func deriveChainHeldByOnePeer(t *testing.T) {
	t.Parallel()

	a, b, c := ticket("1", "cc-1-a"), ticket("2", "cc-2-b"), ticket("3", "cc-3-c")
	in := plan.Input{
		Tickets: []plan.Ticket{c, b, a},
		Obs: plan.Observation{
			PRs: openPRs(a, b, c),
			ConflictsWithPeer: map[plan.BranchRef]map[plan.BranchRef]bool{
				key(a.Branch): {key(b.Branch): true},
				key(b.Branch): {key(a.Branch): true, key(c.Branch): true},
				key(c.Branch): {key(b.Branch): true},
			},
		},
		Runs: map[plan.TicketURL]plan.RunSummary{a.URL: pushedRun(), b.URL: pushedRun(), c.URL: pushedRun()},
	}
	snap := derive(plan.Rules{}, in)

	if got := entry(t, snap, b.URL).Run.Unwrap().ConflictingPeer; got != a.Branch {
		t.Errorf("Derive().Entry(%s).ConflictingPeer = %q, want %q", b.URL, got, a.Branch)
	}
	for _, free := range []plan.Ticket{a, c} {
		if got := entry(t, snap, free.URL).Run.UnwrapOr(plan.RunFact{}).ConflictingPeer; got != "" {
			t.Errorf("Derive().Entry(%s).ConflictingPeer = %q, want none", free.URL, got)
		}
	}
}

func deriveDraftReason(t *testing.T) {
	t.Parallel()

	consumer := plan.Ticket{
		URL: "sandbox://CC-1", Repo: "r", Branch: "cc-1", BlockedBy: []plan.TicketURL{"sandbox://PLA-40"},
	}
	blocker := plan.Ticket{URL: "sandbox://PLA-40", Repo: "other", Branch: "pla-40"}
	pushedAt := time.Date(2026, 8, 20, 11, 0, 0, 0, time.UTC)
	rules := plan.Rules{Checks: map[string]verdict.Predicate{"r": {Success: "CI"}}}

	tests := []struct {
		name    string
		draft   bool
		blocker plan.PRState
		checks  string
		want    string
	}{
		{name: "a ready pull request has no reason", draft: false, blocker: plan.Open, want: ""},
		{
			name: "an open gating blocker keeps it a draft", draft: true, blocker: plan.Open, checks: "SUCCESS",
			want: "waiting on sandbox://PLA-40",
		},
		{
			name: "a merged blocker with red checks keeps it a draft", draft: true, blocker: plan.Merged, checks: "FAILURE",
			want: "waiting on its own checks",
		},
		{
			name: "a gate that says ready names the failed un-draft", draft: true, blocker: plan.Merged, checks: "SUCCESS",
			want: "ready to un-draft; the last gh pr ready call has not taken effect yet",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			in := plan.Input{
				Tickets: []plan.Ticket{consumer, blocker},
				Obs: plan.Observation{
					PRs: map[plan.BranchRef]plan.PR{
						key(consumer.Branch): {
							State: plan.Open, IsDraft: tt.draft, HeadOid: "tip",
							Checks: map[string]plan.CheckState{"CI": {Status: "COMPLETED", Conclusion: tt.checks}},
						},
						{Repo: "other", Branch: blocker.Branch}: {State: tt.blocker},
					},
					BranchTips: map[plan.BranchRef]plan.SHA{key("main"): "main-tip"},
				},
				Runs: map[plan.TicketURL]plan.RunSummary{consumer.URL: pushedRun()},
				Verdict: plan.VerdictFacts{PushRows: map[plan.TicketURL]plan.PushRow{
					consumer.URL: {PushedTip: "tip", BaseBranch: "main", BaseSHAAtPush: "main-tip", PushedAt: pushedAt},
				}},
			}
			got := entry(t, derive(rules, in), consumer.URL)
			wantReady := tt.draft && tt.blocker == plan.Merged && tt.checks == "SUCCESS"
			if got.ReadyToUndraft != wantReady {
				t.Errorf("Derive().Entry(%s).ReadyToUndraft = %t, want %t", consumer.URL, got.ReadyToUndraft, wantReady)
			}
			if !got.OpensAsDraft {
				t.Errorf("Derive().Entry(%s).OpensAsDraft = false, want true", consumer.URL)
			}
			if got.DraftReason != tt.want {
				t.Errorf("Derive().Entry(%s).DraftReason = %q, want %q", consumer.URL, got.DraftReason, tt.want)
			}
		})
	}
}

func TestSnapshot_Offers(t *testing.T) {
	t.Parallel()

	tk := ticket("1", "cc-1")
	snap := derive(plan.Rules{}, plan.Input{Tickets: []plan.Ticket{tk}})

	tests := []struct {
		name string
		url  plan.TicketURL
		verb plan.Verb
		want bool
	}{
		{name: "a ready ticket offers launch", url: tk.URL, verb: plan.VerbLaunch, want: true},
		{name: "a ready ticket does not offer kill", url: tk.URL, verb: plan.VerbKill},
		{name: "an unknown ticket offers nothing", url: "sandbox://CC-404", verb: plan.VerbLaunch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := snap.Offers(tt.url, tt.verb); got != tt.want {
				t.Errorf("Offers(%s, %s) = %t, want %t", tt.url, tt.verb, got, tt.want)
			}
		})
	}
}
