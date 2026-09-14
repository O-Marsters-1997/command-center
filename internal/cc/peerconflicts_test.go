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
	tips := map[string]string{"a": "sha-a", "b": "sha-b", "c": "sha-c"}
	prev := Observation{
		BranchTips: map[string]string{"a": "sha-a", "b": "sha-b", "c": "sha-c"},
		ConflictsWithPeer: map[string]map[string]bool{
			"a": {"b": true, "c": false},
			"b": {"a": true, "c": false},
			"c": {"a": false, "b": false},
		},
	}
	calls := 0
	merges := func(context.Context, string, string, string) (bool, error) {
		calls++
		return false, nil
	}

	into := map[string]map[string]bool{}
	if err := recordPeerConflicts(t.Context(), "", branches, tips, prev, into, merges); err != nil {
		t.Fatalf("recordPeerConflicts: %v", err)
	}
	if calls != 0 {
		t.Errorf("calls = %d, want 0: no tip moved since prev", calls)
	}
	if !into["a"]["b"] || into["a"]["c"] || into["b"]["c"] {
		t.Errorf("into = %v, want the prior tick's reads carried over unchanged", into)
	}
}

// TestRecordPeerConflictsRecomputesOnlyPairsWithAMovedTip covers the other acceptance
// criterion: a tick in which one tip moved recomputes only the pairs involving that branch.
func TestRecordPeerConflictsRecomputesOnlyPairsWithAMovedTip(t *testing.T) {
	t.Parallel()

	branches := []string{"a", "b", "c"}
	tips := map[string]string{"a": "sha-a-new", "b": "sha-b", "c": "sha-c"} // a moved
	prev := Observation{
		BranchTips: map[string]string{"a": "sha-a", "b": "sha-b", "c": "sha-c"},
		ConflictsWithPeer: map[string]map[string]bool{
			"a": {"b": true, "c": false},
			"b": {"a": true, "c": false},
			"c": {"a": false, "b": false},
		},
	}
	var recomputed []string
	merges := func(_ context.Context, _ string, tipA, tipB string) (bool, error) {
		recomputed = append(recomputed, tipA+"/"+tipB)
		return false, nil // reports a conflict for every pair it is asked about
	}

	into := map[string]map[string]bool{}
	if err := recordPeerConflicts(t.Context(), "", branches, tips, prev, into, merges); err != nil {
		t.Fatalf("recordPeerConflicts: %v", err)
	}
	if len(recomputed) != 2 {
		t.Fatalf("recomputed = %v, want 2 calls: only the pairs involving a's moved tip", recomputed)
	}
	if into["b"]["c"] {
		t.Errorf(`into["b"]["c"] = true, want the cached clean read carried over`)
	}
	if !into["a"]["b"] || !into["a"]["c"] {
		t.Errorf("into = %v, want a's pairs recomputed as conflicting", into)
	}
}

// TestRecordPeerConflictsWithNoPriorObservation covers the cold-start case: nothing to reuse,
// so every pair is read.
func TestRecordPeerConflictsWithNoPriorObservation(t *testing.T) {
	t.Parallel()

	branches := []string{"a", "b"}
	tips := map[string]string{"a": "sha-a", "b": "sha-b"}
	calls := 0
	merges := func(context.Context, string, string, string) (bool, error) {
		calls++
		return true, nil
	}

	into := map[string]map[string]bool{}
	if err := recordPeerConflicts(t.Context(), "", branches, tips, Observation{}, into, merges); err != nil {
		t.Fatalf("recordPeerConflicts: %v", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1: nothing from a prior tick to reuse", calls)
	}
	if into["a"]["b"] {
		t.Errorf(`into["a"]["b"] = true, want false: merges() reported clean`)
	}
}
