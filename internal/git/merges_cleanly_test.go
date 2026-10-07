package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
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
		got, _, err := MergesCleanly(t.Context(), dir, "main", tt.branch)
		if err != nil {
			t.Fatalf("MergesCleanly(main, %s): %v", tt.branch, err)
		}
		if got != tt.want {
			t.Errorf("MergesCleanly(main, %s) = %v, want %v", tt.branch, got, tt.want)
		}
	}
}

func TestMergesCleanlyNamesEveryConflictedPath(t *testing.T) {
	t.Parallel()

	dir := initRepoForGitTest(t)
	commitLine(t, dir, "one\ntwo\nthree\n", "base")
	runGitInTest(t, dir, "checkout", "-q", "-b", "child")
	commitLine(t, dir, "one\nTWO-from-child\nthree\n", "child edit")
	runGitInTest(t, dir, "checkout", "-q", "main")
	commitLine(t, dir, "one\nTWO-from-main\nthree\n", "main edit")

	clean, paths, err := MergesCleanly(t.Context(), dir, "main", "child")
	if err != nil {
		t.Fatalf("MergesCleanly(main, child): %v", err)
	}
	if clean {
		t.Fatal("MergesCleanly(main, child) = clean, want conflicted")
	}
	if want := []string{"f.txt"}; !slices.Equal(paths, want) {
		t.Errorf("MergesCleanly(main, child) paths = %v, want %v", paths, want)
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
