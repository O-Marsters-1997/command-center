package git_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/adapter/git"
	"github.com/O-Marsters-1997/command-center/internal/adapter/git/gittest"
)

func TestDiff(t *testing.T) {
	t.Parallel()

	dir := gittest.Init(t)
	gittest.Commit(t, dir, "keep.txt", "one\ntwo\nthree\n")
	gittest.Commit(t, dir, "gone.txt", "bye\n")
	gittest.Commit(t, dir, "old name.txt", "l1\nl2\nl3\nl4\nl5\nl6\n")
	if err := os.WriteFile(filepath.Join(dir, "img.bin"), []byte{0, 1, 2, 0}, 0o600); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	gittest.Run(t, dir, "add", "img.bin")
	gittest.Run(t, dir, "commit", "-q", "-m", "binary")
	base := gittest.Run(t, dir, "rev-parse", "HEAD")

	gittest.Run(t, dir, "switch", "-q", "-c", "work")
	gittest.Commit(t, dir, "keep.txt", "one\nTWO\nthree\nfour\n")
	gittest.Run(t, dir, "rm", "-q", "gone.txt")
	gittest.Run(t, dir, "mv", "old name.txt", "new name.txt")
	gittest.Commit(t, dir, "added.txt", "a\n")
	if err := os.WriteFile(filepath.Join(dir, "img.bin"), []byte{9, 0, 9, 0}, 0o600); err != nil {
		t.Fatalf("rewrite binary: %v", err)
	}
	gittest.Run(t, dir, "add", "-A")
	gittest.Run(t, dir, "commit", "-q", "-m", "all")

	got, err := git.Diff(t.Context(), dir, base, "work")
	if err != nil {
		t.Fatalf("Diff(%q, work) error = %v", base, err)
	}

	want := []git.FileDiff{
		{Path: "added.txt", Added: 1, Hunks: []git.Hunk{{
			Header: "@@ -0,0 +1 @@",
			Lines:  []git.DiffLine{{Kind: git.LineAdded, NewLine: 1, Text: "a"}},
		}}},
		{Path: "gone.txt", Deleted: 1, Hunks: []git.Hunk{{
			Header: "@@ -1 +0,0 @@",
			Lines:  []git.DiffLine{{Kind: git.LineDeleted, OldLine: 1, Text: "bye"}},
		}}},
		{Path: "img.bin", Binary: true},
		{Path: "keep.txt", Added: 2, Deleted: 1, Hunks: []git.Hunk{{
			Header: "@@ -1,3 +1,4 @@",
			Lines: []git.DiffLine{
				{Kind: git.LineContext, OldLine: 1, NewLine: 1, Text: "one"},
				{Kind: git.LineDeleted, OldLine: 2, Text: "two"},
				{Kind: git.LineAdded, NewLine: 2, Text: "TWO"},
				{Kind: git.LineContext, OldLine: 3, NewLine: 3, Text: "three"},
				{Kind: git.LineAdded, NewLine: 4, Text: "four"},
			},
		}}},
		{Path: "new name.txt", OldPath: "old name.txt"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Diff(%q, work) mismatch (-want +got):\n%s", base, diff)
	}
}

func TestDiff_unknownRef(t *testing.T) {
	t.Parallel()

	dir := gittest.Init(t)
	if _, err := git.Diff(t.Context(), dir, "nope", "main"); err == nil {
		t.Error("Diff(nope, main) error = nil, want one")
	}
}
