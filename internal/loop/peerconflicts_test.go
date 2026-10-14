package loop

import (
	"context"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func peerRef(branch string) plan.BranchRef { return plan.BranchRef{Repo: "r", Branch: branch} }

func TestRecordPeerConflictsReusesUnmovedTips(t *testing.T) {
	t.Parallel()

	a, b, c := peerRef("a"), peerRef("b"), peerRef("c")
	tips := map[plan.BranchRef]plan.SHA{a: "sha-a", b: "sha-b", c: "sha-c"}
	prev := plan.Observation{
		BranchTips: map[plan.BranchRef]plan.SHA{a: "sha-a", b: "sha-b", c: "sha-c"},
		ConflictsWithPeer: map[plan.BranchRef]map[plan.BranchRef]bool{
			a: {b: true, c: false},
			b: {a: true, c: false},
			c: {a: false, b: false},
		},
	}
	calls := 0
	merges := func(context.Context, string, string, string) (bool, error) {
		calls++
		return false, nil
	}

	into := map[plan.BranchRef]map[plan.BranchRef]bool{}
	scan := peerScan{
		repo: "r", branches: []string{"a", "b", "c"}, tips: tips, prev: prev, into: into, merges: merges,
	}
	if err := recordPeerConflicts(t.Context(), scan); err != nil {
		t.Fatalf("recordPeerConflicts: %v", err)
	}
	if calls != 0 {
		t.Errorf("calls = %d, want 0: no tip moved since prev", calls)
	}
	if !into[a][b] || into[a][c] || into[b][c] {
		t.Errorf("into = %v, want the prior tick's reads carried over unchanged", into)
	}
}

func TestRecordPeerConflictsRecomputesOnlyPairsWithAMovedTip(t *testing.T) {
	t.Parallel()

	a, b, c := peerRef("a"), peerRef("b"), peerRef("c")
	tips := map[plan.BranchRef]plan.SHA{a: "sha-a-new", b: "sha-b", c: "sha-c"}
	prev := plan.Observation{
		BranchTips: map[plan.BranchRef]plan.SHA{a: "sha-a", b: "sha-b", c: "sha-c"},
		ConflictsWithPeer: map[plan.BranchRef]map[plan.BranchRef]bool{
			a: {b: true, c: false},
			b: {a: true, c: false},
			c: {a: false, b: false},
		},
	}
	var recomputed []string
	merges := func(_ context.Context, _ string, tipA, tipB string) (bool, error) {
		recomputed = append(recomputed, tipA+"/"+tipB)
		return false, nil
	}

	into := map[plan.BranchRef]map[plan.BranchRef]bool{}
	scan := peerScan{
		repo: "r", branches: []string{"a", "b", "c"}, tips: tips, prev: prev, into: into, merges: merges,
	}
	if err := recordPeerConflicts(t.Context(), scan); err != nil {
		t.Fatalf("recordPeerConflicts: %v", err)
	}
	if len(recomputed) != 2 {
		t.Fatalf("recomputed = %v, want 2 calls: only the pairs involving a's moved tip", recomputed)
	}
	if into[b][c] {
		t.Errorf(`into[b][c] = true, want the cached clean read carried over`)
	}
	if !into[a][b] || !into[a][c] {
		t.Errorf("into = %v, want a's pairs recomputed as conflicting", into)
	}
}

func TestRecordPeerConflictsWithNoPriorObservation(t *testing.T) {
	t.Parallel()

	a, b := peerRef("a"), peerRef("b")
	tips := map[plan.BranchRef]plan.SHA{a: "sha-a", b: "sha-b"}
	calls := 0
	merges := func(context.Context, string, string, string) (bool, error) {
		calls++
		return true, nil
	}

	into := map[plan.BranchRef]map[plan.BranchRef]bool{}
	scan := peerScan{repo: "r", branches: []string{"a", "b"}, tips: tips, into: into, merges: merges}
	if err := recordPeerConflicts(t.Context(), scan); err != nil {
		t.Fatalf("recordPeerConflicts: %v", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1: nothing from a prior tick to reuse", calls)
	}
	if into[a][b] {
		t.Errorf(`into["a"]["b"] = true, want false: merges() reported clean`)
	}
}
