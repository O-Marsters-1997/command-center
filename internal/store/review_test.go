package store_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
)

func reviewFixture(t *testing.T) (*storepkg.Store, plan.ReviewComment) {
	t.Helper()
	st := openStore(t)
	one := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1"}
	two := storepkg.Ticket{URL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2"}
	storetest.Tickets(t, st, one, two)
	comment := plan.ReviewComment{
		Tip: "abc", Path: "a.go", Line: 3, Side: plan.SideNew, Hunk: "@@ -1 +1 @@\n+x", Body: "why",
	}
	id, err := st.AddReviewComment(t.Context(), storepkg.NewReviewComment{
		Ticket: "sandbox://CC-1", ReviewComment: comment, At: testNow,
	})
	if err != nil {
		t.Fatalf("AddReviewComment: %v", err)
	}
	comment.ID = id
	return st, comment
}

func TestStore_ReviewComments(t *testing.T) {
	t.Parallel()

	t.Run("comments are scoped to their ticket", func(t *testing.T) {
		t.Parallel()
		st, comment := reviewFixture(t)
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
		t.Parallel()
		st, comment := reviewFixture(t)
		comment.Body = ""
		_, err := st.AddReviewComment(t.Context(), storepkg.NewReviewComment{
			Ticket: "sandbox://CC-1", ReviewComment: comment, At: testNow,
		})
		if err == nil {
			t.Error("AddReviewComment(empty body) = nil, want the CHECK to refuse it")
		}
	})

	t.Run("request changes clears one ticket and queues one follow-up", func(t *testing.T) {
		t.Parallel()
		st, comment := reviewFixture(t)
		other := comment
		other.Body = "elsewhere"
		if _, err := st.AddReviewComment(t.Context(), storepkg.NewReviewComment{
			Ticket: "sandbox://CC-2", ReviewComment: other, At: testNow,
		}); err != nil {
			t.Fatalf("AddReviewComment(CC-2): %v", err)
		}
		payload := storepkg.FollowUpPayload{Message: "fix it"}
		if err := st.RequestChanges(t.Context(), "sandbox://CC-1", payload, testNow); err != nil {
			t.Fatalf("RequestChanges(CC-1): %v", err)
		}
		if got, err := st.ReviewComments(t.Context(), "sandbox://CC-1"); err != nil || len(got) != 0 {
			t.Errorf("CC-1 comments after request = %v, %v, want none", got, err)
		}
		if got, err := st.ReviewComments(t.Context(), "sandbox://CC-2"); err != nil || len(got) != 1 {
			t.Errorf("CC-2 comments after request = %v, %v, want 1", got, err)
		}
		intents, err := st.PendingVerbIntents(t.Context(), plan.VerbFollowUp)
		if err != nil || len(intents) != 1 || storepkg.DecodeFollowUpPayload(intents[0].Payload).Message != "fix it" {
			t.Errorf("PendingVerbIntents(follow-up) = %v, %v, want one carrying the message", intents, err)
		}
	})

	t.Run("delete only reaches the named ticket", func(t *testing.T) {
		t.Parallel()
		st, comment := reviewFixture(t)
		id := comment.ID
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
