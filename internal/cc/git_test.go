package cc

import (
	"maps"
	"slices"
	"testing"
)

func TestParseWorktrees(t *testing.T) {
	t.Parallel()

	porcelain := "worktree /repos/cc-sandbox\n" +
		"HEAD 3f1a9c0d2b7e4a6f8c1d5e9b0a2c4d6e8f0a1b2c\n" +
		"branch refs/heads/main\n" +
		"\n" +
		"worktree /repos/cc-sandbox-cc-1-first\n" +
		"HEAD 9b0a2c4d6e8f0a1b2c3f1a9c0d2b7e4a6f8c1d5e\n" +
		"branch refs/heads/cc-1-first\n" +
		"\n" +
		"worktree /repos/cc-sandbox-detached\n" +
		"HEAD aa11bb22cc33dd44ee55ff6677889900aabbccdd\n" +
		"detached\n"

	want := map[string]string{
		"main":       "/repos/cc-sandbox",
		"cc-1-first": "/repos/cc-sandbox-cc-1-first",
	}
	got, detached := parseWorktrees([]byte(porcelain))
	if !maps.Equal(got, want) {
		t.Errorf("parseWorktrees() = %v, want %v", got, want)
	}
	if wantDetached := []string{"/repos/cc-sandbox-detached"}; !slices.Equal(detached, wantDetached) {
		t.Errorf("parseWorktrees() detached = %v, want %v", detached, wantDetached)
	}
}

// TestBranchKeyDisambiguatesPerRepo covers issue #85's fourth incident's own regression, generalised
// to every ticket branch, not just main: two repos can hold the same branch name, so the plain name
// would let one repo's fact stomp the other's the moment both are configured (draft_gate.txtar and
// ADR 4 are the same collision end to end).
func TestBranchKeyDisambiguatesPerRepo(t *testing.T) {
	t.Parallel()

	if got, want := branchKey("repo", "main"), branchKey("services", "main"); got == want {
		t.Errorf("branchKey(%q) == branchKey(%q) == %q, want distinct keys per repo", "repo", "services", got)
	}
	if got, want := branchKey("repo", "cc-1-x"), branchKey("peer", "cc-1-x"); got == want {
		t.Errorf("branchKey(%q) == branchKey(%q) == %q, want distinct keys per repo", "repo", "peer", got)
	}
}
