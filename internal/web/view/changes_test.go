package view_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/adapter/git"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

func TestHunkGaps(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		hunks []git.Hunk
		want  []int
	}{
		{name: "no hunks", want: []int{}},
		{name: "first hunk at line 1", hunks: []git.Hunk{{NewStart: 1, NewLines: 3}}, want: []int{0}},
		{name: "first hunk past line 1", hunks: []git.Hunk{{NewStart: 11, NewLines: 4}}, want: []int{10}},
		{
			name:  "later hunk counts from the previous hunk's end",
			hunks: []git.Hunk{{NewStart: 11, NewLines: 4}, {NewStart: 40, NewLines: 2}},
			want:  []int{10, 25},
		},
		{
			name:  "adjacent hunks leave no gap",
			hunks: []git.Hunk{{NewStart: 1, NewLines: 5}, {NewStart: 6, NewLines: 2}},
			want:  []int{0, 0},
		},
		{
			name:  "a pure deletion starts after the line git names",
			hunks: []git.Hunk{{NewStart: 9, NewLines: 0}, {NewStart: 20, NewLines: 1}},
			want:  []int{9, 10},
		},
		{name: "a deleted file has no gap", hunks: []git.Hunk{{NewStart: 0, NewLines: 0}}, want: []int{0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if diff := cmp.Diff(tt.want, view.HunkGaps(tt.hunks)); diff != "" {
				t.Errorf("hunkGaps mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestChangedFile_Bar(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		added, deleted int
		want           [5]string
	}{
		{name: "nothing changed", want: [5]string{}},
		{name: "all added", added: 10, want: [5]string{"added", "added", "added", "added", "added"}},
		{name: "all deleted", deleted: 10, want: [5]string{"deleted", "deleted", "deleted", "deleted", "deleted"}},
		{
			name: "rounds to the nearest cell", added: 1, deleted: 1,
			want: [5]string{"added", "added", "added", "deleted", "deleted"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := view.ChangedFile{FileDiff: git.FileDiff{Added: tt.added, Deleted: tt.deleted}}
			if diff := cmp.Diff(tt.want, f.Bar()); diff != "" {
				t.Errorf("Bar() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestChangedFile_DiffHash(t *testing.T) {
	t.Parallel()
	file := func(text string) view.ChangedFile {
		return view.ChangedFile{FileDiff: git.FileDiff{Path: "a.go", Hunks: []git.Hunk{
			{Header: "@@ -1 +1 @@", Lines: []git.DiffLine{{Kind: git.LineAdded, NewLine: 1, Text: text}}},
		}}}
	}
	if a, b := file("x").DiffHash(), file("x").DiffHash(); a != b {
		t.Errorf("DiffHash() of the same diff = %q then %q, want equal", a, b)
	}
	if a, b := file("x").DiffHash(), file("y").DiffHash(); a == b {
		t.Errorf("DiffHash() = %q for different hunks, want them to differ", a)
	}
}

func TestBuildTree(t *testing.T) {
	t.Parallel()
	paths := []string{
		"README.md",
		"internal/loop/tick.go",
		"internal/loop/testdata/a.json",
		"web/src/deep/only/leaf.ts",
		"web/app.css",
		"cmd/cc/main.go",
	}
	files := make([]view.ChangedFile, len(paths))
	for i, p := range paths {
		files[i].Path = p
	}

	want := view.TreeDir{
		Files: []int{0},
		Dirs: []view.TreeDir{
			{Name: "cmd/cc", Path: "cmd/cc", Files: []int{5}},
			{Name: "internal/loop", Path: "internal/loop", Files: []int{1}, Dirs: []view.TreeDir{
				{Name: "testdata", Path: "internal/loop/testdata", Files: []int{2}},
			}},
			{Name: "web", Path: "web", Files: []int{4}, Dirs: []view.TreeDir{
				{Name: "src/deep/only", Path: "web/src/deep/only", Files: []int{3}},
			}},
		},
	}
	if diff := cmp.Diff(want, view.BuildTree(files)); diff != "" {
		t.Errorf("buildTree mismatch (-want +got):\n%s", diff)
	}
}
