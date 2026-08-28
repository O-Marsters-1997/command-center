package cc

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestMergesCleanly(t *testing.T) {
	t.Parallel()

	dir := initRepoForGitTest(t)
	commitLine(t, dir, "one\ntwo\nthree\n", "base")
	runGitInTest(t, dir, "checkout", "-q", "-b", "child")
	commitLine(t, dir, "one\nTWO-from-child\nthree\n", "child edit")
	runGitInTest(t, dir, "checkout", "-q", "-b", "elsewhere", "main")
	commitLine(t, dir, "one\ntwo\nthree\nfour\n", "an edit that does not overlap")
	runGitInTest(t, dir, "checkout", "-q", "main")
	commitLine(t, dir, "one\nTWO-from-main\nthree\n", "main edit")

	for _, tt := range []struct {
		branch string
		want   bool
	}{
		{branch: "child", want: false},
		{branch: "elsewhere", want: true},
	} {
		got, err := MergesCleanly(t.Context(), dir, "main", tt.branch)
		if err != nil {
			t.Fatalf("MergesCleanly(main, %s): %v", tt.branch, err)
		}
		if got != tt.want {
			t.Errorf("MergesCleanly(main, %s) = %v, want %v", tt.branch, got, tt.want)
		}
	}
}

// TestConflictedBase is the launch gate's own decision: what a cut would be made from, and
// whether that base already carries a conflict a child would inherit.
func TestConflictedBase(t *testing.T) {
	t.Parallel()

	root := plan.Ticket{URL: "sandbox://CC-1", Repo: "r", Branch: "cc-1-first"}
	child := plan.Ticket{
		URL: "sandbox://CC-2", Repo: "r", Branch: "cc-2-second",
		BlockedBy: []string{"sandbox://CC-1"},
	}
	byURL := map[string]plan.Ticket{root.URL: root, child.URL: child}
	stacked := plan.Unlock{Unlocked: true, BaseBranch: "cc-1-first"}
	offMain := plan.Unlock{Unlocked: true, BaseBranch: "main"}
	locked := plan.Unlock{Blocking: []string{"sandbox://CC-1"}}

	conflicts := Observation{ConflictsWithBase: map[string]bool{"cc-1-first": true}, MidMerge: map[string]bool{}}
	midMerge := Observation{ConflictsWithBase: map[string]bool{}, MidMerge: map[string]bool{"cc-1-first": true}}
	clean := Observation{ConflictsWithBase: map[string]bool{"cc-1-first": false}, MidMerge: map[string]bool{}}

	tests := []struct {
		name   string
		task   plan.Ticket
		unlock plan.Unlock
		obs    Observation
		want   string
	}{
		{name: "a base that does not merge cleanly", task: child, unlock: stacked, obs: conflicts, want: "cc-1-first"},
		{name: "a base left mid-merge", task: child, unlock: stacked, obs: midMerge, want: "cc-1-first"},
		{name: "a clean base", task: child, unlock: stacked, obs: clean, want: ""},
		{name: "main is never the conflicted base", task: root, unlock: offMain, obs: conflicts, want: ""},
		{
			name: "a locked row is judged on the base it would get once unlocked",
			task: child, unlock: locked, obs: conflicts, want: "cc-1-first",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := conflictedBase(tt.task, byURL, tt.unlock, true, tt.obs); got != tt.want {
				t.Errorf("conflictedBase() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestConflictedBaseIgnoresAConflictWithoutStacking: with stacking off every cut is from main,
// so a blocker's own conflict is not this row's problem.
func TestConflictedBaseIgnoresAConflictWithoutStacking(t *testing.T) {
	t.Parallel()

	root := plan.Ticket{URL: "sandbox://CC-1", Repo: "r", Branch: "cc-1-first"}
	child := plan.Ticket{
		URL: "sandbox://CC-2", Repo: "r", Branch: "cc-2-second",
		BlockedBy: []string{"sandbox://CC-1"},
	}
	byURL := map[string]plan.Ticket{root.URL: root, child.URL: child}
	obs := Observation{ConflictsWithBase: map[string]bool{"cc-1-first": true}}

	if got := conflictedBase(child, byURL, plan.Unlock{Blocking: []string{"sandbox://CC-1"}}, false, obs); got != "" {
		t.Errorf("conflictedBase() = %q, want \"\": an unstacked cut is from main", got)
	}
}

func commitLine(t *testing.T, dir, body, msg string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitInTest(t, dir, "add", "f.txt")
	runGitInTest(t, dir, "commit", "-q", "-m", msg)
}

func runGitInTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(osEnviron(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}
