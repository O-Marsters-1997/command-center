package cc

import (
	"context"
	"testing"
)

// TestRecordPeerConflictsReusesUnmovedTips covers the observe-phase cache (#180,
// docs/adr/0010-one-conflicting-peer-at-a-time.md): a tick in which no tracked tip moved makes
// no merge-tree call for any peer pair.
func TestRecordPeerConflictsReusesUnmovedTips(t *testing.T) {
	t.Parallel()

	branches := []string{"a", "b", "c"}
	tips := map[string]string{
		branchKey("r", "a"): "sha-a", branchKey("r", "b"): "sha-b", branchKey("r", "c"): "sha-c",
	}
	prev := Observation{
		BranchTips: map[string]string{
			branchKey("r", "a"): "sha-a", branchKey("r", "b"): "sha-b", branchKey("r", "c"): "sha-c",
		},
		ConflictsWithPeer: map[string]map[string]bool{
			branchKey("r", "a"): {branchKey("r", "b"): true, branchKey("r", "c"): false},
			branchKey("r", "b"): {branchKey("r", "a"): true, branchKey("r", "c"): false},
			branchKey("r", "c"): {branchKey("r", "a"): false, branchKey("r", "b"): false},
		},
	}
	calls := 0
	merges := func(context.Context, string, string, string) (bool, []string, error) {
		calls++
		return false, nil, nil
	}

	into := map[string]map[string]bool{}
	if err := recordPeerConflicts(t.Context(), "", "r", branches, tips, prev, into, merges); err != nil {
		t.Fatalf("recordPeerConflicts: %v", err)
	}
	if calls != 0 {
		t.Errorf("calls = %d, want 0: no tip moved since prev", calls)
	}
	a, b, c := branchKey("r", "a"), branchKey("r", "b"), branchKey("r", "c")
	if !into[a][b] || into[a][c] || into[b][c] {
		t.Errorf("into = %v, want the prior tick's reads carried over unchanged", into)
	}
}

// TestRecordPeerConflictsRecomputesOnlyPairsWithAMovedTip covers the other acceptance
// criterion: a tick in which one tip moved recomputes only the pairs involving that branch.
func TestRecordPeerConflictsRecomputesOnlyPairsWithAMovedTip(t *testing.T) {
	t.Parallel()

	branches := []string{"a", "b", "c"}
	tips := map[string]string{
		branchKey("r", "a"): "sha-a-new", branchKey("r", "b"): "sha-b", branchKey("r", "c"): "sha-c",
	} // a moved
	prev := Observation{
		BranchTips: map[string]string{
			branchKey("r", "a"): "sha-a", branchKey("r", "b"): "sha-b", branchKey("r", "c"): "sha-c",
		},
		ConflictsWithPeer: map[string]map[string]bool{
			branchKey("r", "a"): {branchKey("r", "b"): true, branchKey("r", "c"): false},
			branchKey("r", "b"): {branchKey("r", "a"): true, branchKey("r", "c"): false},
			branchKey("r", "c"): {branchKey("r", "a"): false, branchKey("r", "b"): false},
		},
	}
	var recomputed []string
	merges := func(_ context.Context, _ string, tipA, tipB string) (bool, []string, error) {
		recomputed = append(recomputed, tipA+"/"+tipB)
		return false, nil, nil // reports a conflict for every pair it is asked about
	}

	into := map[string]map[string]bool{}
	if err := recordPeerConflicts(t.Context(), "", "r", branches, tips, prev, into, merges); err != nil {
		t.Fatalf("recordPeerConflicts: %v", err)
	}
	if len(recomputed) != 2 {
		t.Fatalf("recomputed = %v, want 2 calls: only the pairs involving a's moved tip", recomputed)
	}
	a, b, c := branchKey("r", "a"), branchKey("r", "b"), branchKey("r", "c")
	if into[b][c] {
		t.Errorf(`into[b][c] = true, want the cached clean read carried over`)
	}
	if !into[a][b] || !into[a][c] {
		t.Errorf("into = %v, want a's pairs recomputed as conflicting", into)
	}
}

// TestRecordPeerConflictsWithNoPriorObservation covers the cold-start case: nothing to reuse,
// so every pair is read.
func TestRecordPeerConflictsWithNoPriorObservation(t *testing.T) {
	t.Parallel()

	branches := []string{"a", "b"}
	tips := map[string]string{branchKey("r", "a"): "sha-a", branchKey("r", "b"): "sha-b"}
	calls := 0
	merges := func(context.Context, string, string, string) (bool, []string, error) {
		calls++
		return true, nil, nil
	}

	into := map[string]map[string]bool{}
	if err := recordPeerConflicts(t.Context(), "", "r", branches, tips, Observation{}, into, merges); err != nil {
		t.Fatalf("recordPeerConflicts: %v", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1: nothing from a prior tick to reuse", calls)
	}
	if into[branchKey("r", "a")][branchKey("r", "b")] {
		t.Errorf(`into["a"]["b"] = true, want false: merges() reported clean`)
	}
}
