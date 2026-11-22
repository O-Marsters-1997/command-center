package view_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

func TestNewRequestChanges(t *testing.T) {
	t.Parallel()
	comments := []plan.ReviewComment{
		{Path: "b.go", Line: 2, Body: "b2"},
		{Path: "a.go", Line: 9, Body: "a9"},
		{Path: "a.go", Line: 3, Body: "a3"},
	}

	t.Run("sorts comments by path then line", func(t *testing.T) {
		t.Parallel()
		got := view.NewRequestChanges("/s/o/n/1", "0123456789", true, "", "", comments)
		var order []string
		for _, c := range got.Comments {
			order = append(order, c.Body)
		}
		if diff := cmp.Diff([]string{"a3", "a9", "b2"}, order); diff != "" {
			t.Errorf("Comments order mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("the preview is the composed follow-up with the owner's text marked", func(t *testing.T) {
		t.Parallel()
		got := view.NewRequestChanges("/s/o/n/1", "0123456789", true, "", "  mind the API ", comments)
		want := plan.FollowUpPrompt{
			Message: "mind the API", Comments: 3,
			Attachment: plan.RunIDPlaceholder + "." + plan.ReviewAttachmentName,
		}.Compose()
		parts := got.Preview()
		if joined := parts.Before + parts.Note + parts.After; joined != want {
			t.Errorf("Preview joined = %q, want Compose() = %q", joined, want)
		}
		if parts.Note != "mind the API" {
			t.Errorf("Preview note = %q, want %q", parts.Note, "mind the API")
		}
		if !strings.Contains(got.Subtitle(), "Branch tip 01234567 · 3 pending comments") {
			t.Errorf("Subtitle() = %q, want the tip and count", got.Subtitle())
		}
	})

	t.Run("a note that repeats the boilerplate is marked in its own slot", func(t *testing.T) {
		t.Parallel()
		got := view.NewRequestChanges("/s/o/n/1", "0123456789", true, "", "branch", comments).Preview()
		inSlot := strings.HasSuffix(got.Before, "on this branch.\n\n") &&
			strings.HasPrefix(got.After, "\n\nAddress every review")
		if !inSlot {
			t.Errorf("Preview() = %+v, want the note between the preamble and the attachment text", got)
		}
	})

	t.Run("send is allowed only with text or comments and where follow-up is offered", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name     string
			offered  bool
			note     string
			comments []plan.ReviewComment
			want     bool
			why      string
		}{
			{name: "nothing to send", offered: true, want: false, why: "Write what to change, or comment on the diff first."},
			{name: "note only", offered: true, note: "x", want: true},
			{name: "comments only", offered: true, comments: comments, want: true},
			{name: "not offered", offered: false, note: "x", want: false, why: "merged"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				got := view.NewRequestChanges("/s/o/n/1", "0123456789", tt.offered, "merged", tt.note, tt.comments)
				if got.CanSend != tt.want || got.Why != tt.why {
					t.Errorf("CanSend, Why = %v, %q, want %v, %q", got.CanSend, got.Why, tt.want, tt.why)
				}
			})
		}
	})
}
