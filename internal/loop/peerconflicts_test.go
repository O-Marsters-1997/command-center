package loop

import (
	"context"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestRecordPeerConflictsReusesUnmovedTips(t *testing.T) {
	t.Parallel()

	branches := []string{"a", "b", "c"}
	tips := map[string]string{
		plan.BranchKey("r", "a"): "sha-a", plan.BranchKey("r", "b"): "sha-b", plan.BranchKey("r", "c"): "sha-c",
	}
	prev := plan.Observation{
		BranchTips: map[string]string{
			plan.BranchKey("r", "a"): "sha-a", plan.BranchKey("r", "b"): "sha-b", plan.BranchKey("r", "c"): "sha-c",
		},
		ConflictsWithPeer: map[string]map[string]bool{
			plan.BranchKey("r", "a"): {plan.BranchKey("r", "b"): true, plan.BranchKey("r", "c"): false},
			plan.BranchKey("r", "b"): {plan.BranchKey("r", "a"): true, plan.BranchKey("r", "c"): false},
			plan.BranchKey("r", "c"): {plan.BranchKey("r", "a"): false, plan.BranchKey("r", "b"): false},
		},
	}
	calls := 0
	merges := func(context.Context, string, string, string) (bool, error) {
		calls++
		return false, nil
	}

	into := map[string]map[string]bool{}
	if err := recordPeerConflicts(t.Context(), "", "r", branches, tips, prev, into, merges); err != nil {
		t.Fatalf("recordPeerConflicts: %v", err)
	}
	if calls != 0 {
		t.Errorf("calls = %d, want 0: no tip moved since prev", calls)
	}
	a, b, c := plan.BranchKey("r", "a"), plan.BranchKey("r", "b"), plan.BranchKey("r", "c")
	if !into[a][b] || into[a][c] || into[b][c] {
		t.Errorf("into = %v, want the prior tick's reads carried over unchanged", into)
	}
}

func TestRecordPeerConflictsRecomputesOnlyPairsWithAMovedTip(t *testing.T) {
	t.Parallel()

	branches := []string{"a", "b", "c"}
	tips := map[string]string{
		plan.BranchKey("r", "a"): "sha-a-new", plan.BranchKey("r", "b"): "sha-b", plan.BranchKey("r", "c"): "sha-c",
	} // a moved
	prev := plan.Observation{
		BranchTips: map[string]string{
			plan.BranchKey("r", "a"): "sha-a", plan.BranchKey("r", "b"): "sha-b", plan.BranchKey("r", "c"): "sha-c",
		},
		ConflictsWithPeer: map[string]map[string]bool{
			plan.BranchKey("r", "a"): {plan.BranchKey("r", "b"): true, plan.BranchKey("r", "c"): false},
			plan.BranchKey("r", "b"): {plan.BranchKey("r", "a"): true, plan.BranchKey("r", "c"): false},
			plan.BranchKey("r", "c"): {plan.BranchKey("r", "a"): false, plan.BranchKey("r", "b"): false},
		},
	}
	var recomputed []string
	merges := func(_ context.Context, _ string, tipA, tipB string) (bool, error) {
		recomputed = append(recomputed, tipA+"/"+tipB)
		return false, nil // reports a conflict for every pair it is asked about
	}

	into := map[string]map[string]bool{}
	if err := recordPeerConflicts(t.Context(), "", "r", branches, tips, prev, into, merges); err != nil {
		t.Fatalf("recordPeerConflicts: %v", err)
	}
	if len(recomputed) != 2 {
		t.Fatalf("recomputed = %v, want 2 calls: only the pairs involving a's moved tip", recomputed)
	}
	a, b, c := plan.BranchKey("r", "a"), plan.BranchKey("r", "b"), plan.BranchKey("r", "c")
	if into[b][c] {
		t.Errorf(`into[b][c] = true, want the cached clean read carried over`)
	}
	if !into[a][b] || !into[a][c] {
		t.Errorf("into = %v, want a's pairs recomputed as conflicting", into)
	}
}

func TestRecordPeerConflictsWithNoPriorObservation(t *testing.T) {
	t.Parallel()

	branches := []string{"a", "b"}
	tips := map[string]string{plan.BranchKey("r", "a"): "sha-a", plan.BranchKey("r", "b"): "sha-b"}
	calls := 0
	merges := func(context.Context, string, string, string) (bool, error) {
		calls++
		return true, nil
	}

	into := map[string]map[string]bool{}
	if err := recordPeerConflicts(t.Context(), "", "r", branches, tips, plan.Observation{}, into, merges); err != nil {
		t.Fatalf("recordPeerConflicts: %v", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1: nothing from a prior tick to reuse", calls)
	}
	if into[plan.BranchKey("r", "a")][plan.BranchKey("r", "b")] {
		t.Errorf(`into["a"]["b"] = true, want false: merges() reported clean`)
	}
}
