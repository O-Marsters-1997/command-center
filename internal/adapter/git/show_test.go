package git_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/adapter/git"
	"github.com/O-Marsters-1997/command-center/internal/adapter/git/gittest"
)

func TestFileLines(t *testing.T) {
	t.Parallel()

	dir := gittest.Init(t)
	gittest.Commit(t, dir, "f.txt", "one\ntwo\nthree\nfour\n")
	gittest.Commit(t, dir, "f.txt", "ONE\ntwo\nthree\nfour\n")

	tests := []struct {
		name     string
		rev      string
		from, to int
		want     []string
	}{
		{name: "a middle range", rev: "HEAD", from: 2, to: 3, want: []string{"two", "three"}},
		{name: "the whole file", rev: "HEAD", from: 1, to: 4, want: []string{"ONE", "two", "three", "four"}},
		{name: "a range cut short by the end", rev: "HEAD", from: 3, to: 99, want: []string{"three", "four"}},
		{name: "a range past the end", rev: "HEAD", from: 5, to: 9},
		{name: "an earlier revision", rev: "HEAD~1", from: 1, to: 1, want: []string{"one"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := git.FileLines(t.Context(), dir, tt.rev, "f.txt", tt.from, tt.to)
			if err != nil {
				t.Fatalf("FileLines(%s, %d, %d) error = %v", tt.rev, tt.from, tt.to, err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("FileLines(%s, %d, %d) mismatch (-want +got):\n%s", tt.rev, tt.from, tt.to, diff)
			}
		})
	}

	errTests := []struct {
		name     string
		path     string
		from, to int
	}{
		{name: "a range starting before line 1", path: "f.txt", from: 0, to: 2},
		{name: "an inverted range", path: "f.txt", from: 3, to: 2},
		{name: "a missing path", path: "nope.txt", from: 1, to: 1},
	}
	for _, tt := range errTests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got, err := git.FileLines(t.Context(), dir, "HEAD", tt.path, tt.from, tt.to); err == nil {
				t.Errorf("FileLines(%s, %d, %d) = %v, want an error", tt.path, tt.from, tt.to, got)
			}
		})
	}
}
