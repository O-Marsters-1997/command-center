package git_test

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/git"
	"github.com/O-Marsters-1997/command-center/internal/gittest"
)

func TestMergesCleanly(t *testing.T) {
	t.Parallel()

	dir := gittest.Init(t)
	gittest.Commit(t, dir, "f.txt", "one\ntwo\nthree\n")
	gittest.Run(t, dir, "checkout", "-q", "-b", "child")
	gittest.Commit(t, dir, "f.txt", "one\nTWO-from-child\nthree\n")
	gittest.Run(t, dir, "checkout", "-q", "-b", "elsewhere", "main")
	gittest.Commit(t, dir, "f.txt", "one\ntwo\nthree\nfour\n")
	gittest.Run(t, dir, "checkout", "-q", "main")
	gittest.Commit(t, dir, "f.txt", "one\nTWO-from-main\nthree\n")

	for _, tt := range []struct {
		branch string
		want   bool
	}{
		{branch: "child", want: false},
		{branch: "elsewhere", want: true},
	} {
		got, err := git.MergesCleanly(t.Context(), dir, "main", tt.branch)
		if err != nil {
			t.Fatalf("MergesCleanly(main, %s): %v", tt.branch, err)
		}
		if got != tt.want {
			t.Errorf("MergesCleanly(main, %s) = %v, want %v", tt.branch, got, tt.want)
		}
	}
}
