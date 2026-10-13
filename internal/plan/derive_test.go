package plan_test

import (
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/verdict"
)

func key(branch string) string { return plan.BranchKey("r", branch) }

func ticket(n, branch string, blockedBy ...string) plan.Ticket {
	return plan.Ticket{URL: "sandbox://CC-" + n, Repo: "r", Branch: branch, BlockedBy: blockedBy}
}

func openPRs(tickets ...plan.Ticket) map[string]plan.PR {
	prs := make(map[string]plan.PR, len(tickets))
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

func entry(t *testing.T, snap plan.Snapshot, url string) plan.Entry {
	t.Helper()
	e, ok := snap.Entry(url)
	if !ok {
		t.Fatalf("snapshot has no entry for %s", url)
	}
	return e
}

func TestDeriveConflictedBase(t *testing.T) {
	t.Parallel()

	root := ticket("1", "cc-1-first")
	child := ticket("2", "cc-2-second", root.URL)
	tickets := []plan.Ticket{root, child}

	conflicts := plan.Observation{ConflictsWithBase: map[string]bool{key(root.Branch): true}}
	midMerge := plan.Observation{MidMerge: map[string]bool{key(root.Branch): true}}
	clean := plan.Observation{ConflictsWithBase: map[string]bool{key(root.Branch): false}}
	withPR := func(o plan.Observation) plan.Observation {
		o.PRs = openPRs(root)
		return o
	}

	tests := []struct {
		name     string
		stacking bool
		obs      plan.Observation
		url      string
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
				t.Errorf("ConflictedBase = %q, want %q", got, tt.want)
			}
		})
	}
}

func pushedRun() plan.RunSummary {
	return plan.RunSummary{HasOutcome: true, Outcome: plan.OutcomePush, LogPath: "run.log"}
}

func peerConflicts(pairs ...[2]string) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, p := range pairs {
		if out[key(p[0])] == nil {
			out[key(p[0])] = map[string]bool{}
		}
		out[key(p[0])][key(p[1])] = true
	}
	return out
}

func TestDeriveConflictingPeer(t *testing.T) {
	t.Parallel()

	lower := ticket("1", "cc-1-lower")
	higher := ticket("2", "cc-2-higher")
	both := peerConflicts([2]string{lower.Branch, higher.Branch}, [2]string{higher.Branch, lower.Branch})

	tests := []struct {
		name     string
		tickets  []plan.Ticket
		stacking bool
		prs      map[string]plan.PR
		peers    map[string]map[string]bool
		want     map[string]string
	}{
		{
			name:    "a conflicting pair holds the higher ref behind the lower",
			tickets: []plan.Ticket{higher, lower}, prs: openPRs(lower, higher), peers: both,
			want: map[string]string{higher.URL: lower.Branch},
		},
		{
			name:    "a clean pair holds neither",
			tickets: []plan.Ticket{higher, lower}, prs: openPRs(lower, higher),
			peers: map[string]map[string]bool{key(lower.Branch): {key(higher.Branch): false}},
			want:  map[string]string{},
		},
		{
			name:     "a stacked branch is never a candidate",
			tickets:  []plan.Ticket{lower, ticket("2", higher.Branch, lower.URL)},
			stacking: true, prs: openPRs(lower, higher), peers: both,
			want: map[string]string{},
		},
		{
			name:    "a branch with no open pull request is never a candidate",
			tickets: []plan.Ticket{higher, lower}, prs: openPRs(lower), peers: both,
			want: map[string]string{},
		},
		{
			name: "ref order is by ticket number, not branch string, across a digit-count boundary",
			tickets: []plan.Ticket{
				ticket("100", "cc-100-hundred"), ticket("9", "cc-9-nine"),
			},
			prs: openPRs(ticket("100", "cc-100-hundred"), ticket("9", "cc-9-nine")),
			peers: peerConflicts(
				[2]string{"cc-9-nine", "cc-100-hundred"}, [2]string{"cc-100-hundred", "cc-9-nine"}),
			want: map[string]string{"sandbox://CC-100": "cc-9-nine"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			in := plan.Input{
				Tickets: tt.tickets,
				Obs:     plan.Observation{PRs: tt.prs, ConflictsWithPeer: tt.peers},
				Runs:    map[string]plan.RunSummary{},
			}
			for _, tk := range tt.tickets {
				in.Runs[tk.URL] = pushedRun()
			}
			snap := derive(plan.Rules{Stacking: map[string]bool{"r": tt.stacking}}, in)

			got := map[string]string{}
			for _, tk := range tt.tickets {
				if e := entry(t, snap, tk.URL); e.Run != nil && e.Run.ConflictingPeer != "" {
					got[tk.URL] = e.Run.ConflictingPeer
				}
			}
			if len(got) != len(tt.want) {
				t.Fatalf("held = %v, want %v", got, tt.want)
			}
			for url, peer := range tt.want {
				if got[url] != peer {
					t.Errorf("held[%s] = %q, want %q", url, got[url], peer)
				}
			}
		})
	}
}

func TestDeriveChainDoesNotSerialisePastTheHoldingPeer(t *testing.T) {
	t.Parallel()

	a, b, c := ticket("1", "cc-1-a"), ticket("2", "cc-2-b"), ticket("3", "cc-3-c")
	in := plan.Input{
		Tickets: []plan.Ticket{c, b, a},
		Obs: plan.Observation{
			PRs: openPRs(a, b, c),
			ConflictsWithPeer: map[string]map[string]bool{
				key(a.Branch): {key(b.Branch): true},
				key(b.Branch): {key(a.Branch): true, key(c.Branch): true},
				key(c.Branch): {key(b.Branch): true},
			},
		},
		Runs: map[string]plan.RunSummary{a.URL: pushedRun(), b.URL: pushedRun(), c.URL: pushedRun()},
	}
	snap := derive(plan.Rules{}, in)

	if got := entry(t, snap, b.URL).Run.ConflictingPeer; got != a.Branch {
		t.Errorf("b held by %q, want %q", got, a.Branch)
	}
	for _, free := range []plan.Ticket{a, c} {
		if got := entry(t, snap, free.URL).Run.ConflictingPeer; got != "" {
			t.Errorf("%s held by %q, want unheld", free.Branch, got)
		}
	}
}

func TestDeriveDraftReason(t *testing.T) {
	t.Parallel()

	consumer := plan.Ticket{URL: "sandbox://CC-1", Repo: "r", Branch: "cc-1", BlockedBy: []string{"sandbox://PLA-40"}}
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
					PRs: map[string]plan.PR{
						key(consumer.Branch): {
							State: plan.Open, IsDraft: tt.draft, HeadOid: "tip",
							Checks: map[string]plan.CheckState{"CI": {Status: "COMPLETED", Conclusion: tt.checks}},
						},
						plan.BranchKey("other", blocker.Branch): {State: tt.blocker},
					},
					BranchTips: map[string]string{key("main"): "main-tip"},
				},
				Runs: map[string]plan.RunSummary{consumer.URL: pushedRun()},
				Verdict: plan.VerdictFacts{PushRows: map[string]plan.PushRow{
					consumer.URL: {PushedTip: "tip", BaseBranch: "main", BaseSHAAtPush: "main-tip", PushedAt: pushedAt},
				}},
			}
			got := entry(t, derive(rules, in), consumer.URL)
			wantReady := tt.draft && tt.blocker == plan.Merged && tt.checks == "SUCCESS"
			if got.ReadyToUndraft != wantReady {
				t.Errorf("ReadyToUndraft = %v, want %v", got.ReadyToUndraft, wantReady)
			}
			if !got.OpensAsDraft {
				t.Error("OpensAsDraft = false, want true for a ticket with a gating blocker")
			}
			if got.DraftReason != tt.want {
				t.Errorf("DraftReason = %q, want %q", got.DraftReason, tt.want)
			}
		})
	}
}

func TestSnapshotOffersTheVerbsOfTheRowState(t *testing.T) {
	t.Parallel()

	tk := ticket("1", "cc-1")
	snap := derive(plan.Rules{}, plan.Input{Tickets: []plan.Ticket{tk}})

	if !snap.Offers(tk.URL, plan.VerbLaunch) {
		t.Errorf("a ready ticket must offer %s", plan.VerbLaunch)
	}
	if snap.Offers(tk.URL, plan.VerbKill) {
		t.Errorf("a ready ticket must not offer %s", plan.VerbKill)
	}
	if snap.Offers("sandbox://CC-404", plan.VerbLaunch) {
		t.Error("an unknown ticket must offer nothing")
	}
}
