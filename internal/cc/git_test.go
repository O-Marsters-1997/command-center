package cc

import (
	"maps"
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
	if got := parseWorktrees([]byte(porcelain)); !maps.Equal(got, want) {
		t.Errorf("parseWorktrees() = %v, want %v", got, want)
	}
}
