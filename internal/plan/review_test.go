package plan_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestComposeReview(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		note     string
		comments []plan.ReviewComment
		want     string
	}{
		{name: "no comments with a note", note: " ship it ", want: "Changes requested on this branch.\n\nship it"},
		{
			name: "several files",
			comments: []plan.ReviewComment{
				{Path: "a.go", Line: 3, Side: plan.SideNew, Hunk: "@@ -1 +1 @@\n+x", Body: "why"},
				{Path: "b.go", Line: 9, Side: plan.SideNew, Hunk: "@@ -9 +9 @@\n+y", Body: "rename"},
			},
			want: "Changes requested on this branch." +
				"\n\na.go, new line 3:\n```diff\n@@ -1 +1 @@\n+x\n```\nwhy" +
				"\n\nb.go, new line 9:\n```diff\n@@ -9 +9 @@\n+y\n```\nrename",
		},
		{
			name:     "deleted-side line",
			comments: []plan.ReviewComment{{Path: "a.go", Line: 2, Side: plan.SideOld, Hunk: "@@ -2 +1 @@\n-z", Body: "keep"}},
			want:     "Changes requested on this branch.\n\na.go, old line 2:\n```diff\n@@ -2 +1 @@\n-z\n```\nkeep",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if diff := cmp.Diff(tt.want, plan.ComposeReview(tt.note, tt.comments)); diff != "" {
				t.Errorf("ComposeReview mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
