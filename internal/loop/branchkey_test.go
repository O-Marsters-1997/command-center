package loop

import "testing"

// TestBranchKeyDisambiguatesPerRepo covers issue #85's fourth incident's own regression, generalised
// to every ticket branch, not just main: two repos can hold the same branch name, so the plain name
// would let one repo's fact stomp the other's the moment both are configured (draft_gate.txtar and
// ADR 3 are the same collision end to end).
func TestBranchKeyDisambiguatesPerRepo(t *testing.T) {
	t.Parallel()

	if got, want := branchKey("repo", "main"), branchKey("services", "main"); got == want {
		t.Errorf("branchKey(%q) == branchKey(%q) == %q, want distinct keys per repo", "repo", "services", got)
	}
	if got, want := branchKey("repo", "cc-1-x"), branchKey("peer", "cc-1-x"); got == want {
		t.Errorf("branchKey(%q) == branchKey(%q) == %q, want distinct keys per repo", "repo", "peer", got)
	}
}
