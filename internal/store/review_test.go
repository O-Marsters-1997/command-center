package store_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
)

func TestStore_ReviewComments(t *testing.T) {
	t.Parallel()
	st := openStore(t)
	one := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1"}
	two := storepkg.Ticket{URL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2"}
	storetest.Tickets(t, st, one, two)
	comment := plan.ReviewComment{Tip: "abc", Path: "a.go", Line: 3, Side: plan.SideNew, Hunk: "@@ -1 +1 @@\n+x", Body: "why"}

	id, err := st.AddReviewComment(t.Context(), storepkg.NewReviewComment{Ticket: "sandbox://CC-1", ReviewComment: comment, At: testNow})
	if err != nil {
		t.Fatalf("AddReviewComment: %v", err)
	}
	comment.ID = id

	t.Run("comments are scoped to their ticket", func(t *testing.T) {
		got, err := st.ReviewComments(t.Context(), "sandbox://CC-1")
		if err != nil {
			t.Fatalf("ReviewComments(CC-1): %v", err)
		}
		if diff := cmp.Diff([]plan.ReviewComment{comment}, got); diff != "" {
			t.Errorf("ReviewComments(CC-1) mismatch (-want +got):\n%s", diff)
		}
		other, err := st.ReviewComments(t.Context(), "sandbox://CC-2")
		if err != nil || len(other) != 0 {
			t.Errorf("ReviewComments(CC-2) = %v, %v, want none", other, err)
		}
	})

	t.Run("an empty body is rejected", func(t *testing.T) {
		empty := comment
		empty.Body = ""
		if _, err := st.AddReviewComment(t.Context(), storepkg.NewReviewComment{Ticket: "sandbox://CC-1", ReviewComment: empty, At: testNow}); err == nil {
			t.Error("AddReviewComment(empty body) = nil, want the CHECK to refuse it")
		}
	})

	t.Run("delete only reaches the named ticket", func(t *testing.T) {
		if err := st.DeleteReviewComment(t.Context(), "sandbox://CC-2", id); err != nil {
			t.Fatalf("DeleteReviewComment(CC-2): %v", err)
		}
		if got, err := st.ReviewComments(t.Context(), "sandbox://CC-1"); err != nil || len(got) != 1 {
			t.Fatalf("comments after deleting under CC-2 = %d, %v, want 1", len(got), err)
		}
		if err := st.DeleteReviewComment(t.Context(), "sandbox://CC-1", id); err != nil {
			t.Fatalf("DeleteReviewComment(CC-1): %v", err)
		}
		if got, err := st.ReviewComments(t.Context(), "sandbox://CC-1"); err != nil || len(got) != 0 {
			t.Errorf("comments after delete = %d, %v, want 0", len(got), err)
		}
	})
}
