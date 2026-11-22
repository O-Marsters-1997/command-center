package git_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	gittest.Commit(t, dir, "one.txt", "old\n")
	gittest.Commit(t, dir, "multi.txt", lines(30, nil))
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
	gittest.Commit(t, dir, "one.txt", "new\n")
	gittest.Commit(t, dir, "multi.txt", lines(30, map[int]string{3: "CHANGED3", 25: "CHANGED25"}))
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
		{Path: "added.txt", Kind: git.FileAdded, Added: 1, Hunks: []git.Hunk{{
			Header:   "@@ -0,0 +1 @@",
			OldStart: 0, OldLines: 0, NewStart: 1, NewLines: 1,
			Lines: []git.DiffLine{{Kind: git.LineAdded, NewLine: 1, Text: "a"}},
		}}},
		{Path: "gone.txt", Kind: git.FileDeleted, Deleted: 1, Hunks: []git.Hunk{{
			Header:   "@@ -1 +0,0 @@",
			OldStart: 1, OldLines: 1, NewStart: 0, NewLines: 0,
			Lines: []git.DiffLine{{Kind: git.LineDeleted, OldLine: 1, Text: "bye"}},
		}}},
		{Path: "img.bin", Kind: git.FileModified, Binary: true},
		{Path: "keep.txt", Kind: git.FileModified, Added: 2, Deleted: 1, Hunks: []git.Hunk{{
			Header:   "@@ -1,3 +1,4 @@",
			OldStart: 1, OldLines: 3, NewStart: 1, NewLines: 4,
			Lines: []git.DiffLine{
				{Kind: git.LineContext, OldLine: 1, NewLine: 1, Text: "one"},
				{Kind: git.LineDeleted, OldLine: 2, Text: "two"},
				{Kind: git.LineAdded, NewLine: 2, Text: "TWO"},
				{Kind: git.LineContext, OldLine: 3, NewLine: 3, Text: "three"},
				{Kind: git.LineAdded, NewLine: 4, Text: "four"},
			},
		}}},
		{Path: "multi.txt", Kind: git.FileModified, Added: 2, Deleted: 2, Hunks: []git.Hunk{
			{
				Header:   "@@ -1,6 +1,6 @@",
				OldStart: 1, OldLines: 6, NewStart: 1, NewLines: 6,
				Lines: []git.DiffLine{
					{Kind: git.LineContext, OldLine: 1, NewLine: 1, Text: "line1"},
					{Kind: git.LineContext, OldLine: 2, NewLine: 2, Text: "line2"},
					{Kind: git.LineDeleted, OldLine: 3, Text: "line3"},
					{Kind: git.LineAdded, NewLine: 3, Text: "CHANGED3"},
					{Kind: git.LineContext, OldLine: 4, NewLine: 4, Text: "line4"},
					{Kind: git.LineContext, OldLine: 5, NewLine: 5, Text: "line5"},
					{Kind: git.LineContext, OldLine: 6, NewLine: 6, Text: "line6"},
				},
			},
			{
				Header:   "@@ -22,7 +22,7 @@ line21",
				OldStart: 22, OldLines: 7, NewStart: 22, NewLines: 7,
				Section: "line21",
				Lines: []git.DiffLine{
					{Kind: git.LineContext, OldLine: 22, NewLine: 22, Text: "line22"},
					{Kind: git.LineContext, OldLine: 23, NewLine: 23, Text: "line23"},
					{Kind: git.LineContext, OldLine: 24, NewLine: 24, Text: "line24"},
					{Kind: git.LineDeleted, OldLine: 25, Text: "line25"},
					{Kind: git.LineAdded, NewLine: 25, Text: "CHANGED25"},
					{Kind: git.LineContext, OldLine: 26, NewLine: 26, Text: "line26"},
					{Kind: git.LineContext, OldLine: 27, NewLine: 27, Text: "line27"},
					{Kind: git.LineContext, OldLine: 28, NewLine: 28, Text: "line28"},
				},
			},
		}},
		{Path: "new name.txt", OldPath: "old name.txt", Kind: git.FileRenamed},
		{Path: "one.txt", Kind: git.FileModified, Added: 1, Deleted: 1, Hunks: []git.Hunk{{
			Header:   "@@ -1 +1 @@",
			OldStart: 1, OldLines: 1, NewStart: 1, NewLines: 1,
			Lines: []git.DiffLine{
				{Kind: git.LineDeleted, OldLine: 1, Text: "old"},
				{Kind: git.LineAdded, NewLine: 1, Text: "new"},
			},
		}}},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Diff(%q, work) mismatch (-want +got):\n%s", base, diff)
	}
}

func lines(n int, override map[int]string) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		text, ok := override[i]
		if !ok {
			text = "line" + strconv.Itoa(i)
		}
		b.WriteString(text + "\n")
	}
	return b.String()
}

func TestDiff_unknownRef(t *testing.T) {
	t.Parallel()

	dir := gittest.Init(t)
	if _, err := git.Diff(t.Context(), dir, "nope", "main"); err == nil {
		t.Error("Diff(nope, main) error = nil, want one")
	}
}
