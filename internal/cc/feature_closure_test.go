package cc

import "testing"

func TestCloseUnderBlockedByAllowsABlockerInsideTheFeature(t *testing.T) {
	t.Parallel()

	tickets := []ticketBlockedBy{
		{URL: "a", BlockedBy: []string{"b"}},
		{URL: "b"},
	}
	ticketFeature := fixedFeature(map[string]string{"b": "project:x"})
	got := closeUnderBlockedBy("project:x", tickets, ticketFeature, neverMerged)
	if got != nil {
		t.Fatalf("closeUnderBlockedBy = %+v, want nil", got)
	}
}

// TestCloseUnderBlockedByAllowsAWithdrawnUnmergedBlockerStillTaggedToTheFeature pins the other
// half of issue #235 (already shipped): a blocker withdrawn without merging is never reassigned,
// so it must not manufacture a closure violation just for being momentarily inactive.
func TestCloseUnderBlockedByAllowsAWithdrawnUnmergedBlockerStillTaggedToTheFeature(t *testing.T) {
	t.Parallel()

	tickets := []ticketBlockedBy{{URL: "a", BlockedBy: []string{"withdrawn"}}}
	ticketFeature := fixedFeature(map[string]string{"withdrawn": "project:x"})
	got := closeUnderBlockedBy("project:x", tickets, ticketFeature, neverMerged)
	if got != nil {
		t.Fatalf("closeUnderBlockedBy = %+v, want nil: the blocker is still tagged to project:x", got)
	}
}

func TestCloseUnderBlockedByRefusesABlockerOutsideTheFeature(t *testing.T) {
	t.Parallel()

	tickets := []ticketBlockedBy{
		{URL: "a", BlockedBy: []string{"outside"}},
	}
	ticketFeature := fixedFeature(map[string]string{"outside": "project:y"})
	got := closeUnderBlockedBy("project:x", tickets, ticketFeature, neverMerged)
	if got == nil {
		t.Fatal("closeUnderBlockedBy = nil, want a violation")
		return
	}
	want := FeatureClosureError{Feature: "project:x", URL: "a", Blocker: "outside", BlockerFeature: "project:y"}
	if *got != want {
		t.Errorf("closeUnderBlockedBy = %+v, want %+v", *got, want)
	}
}

func TestCloseUnderBlockedByAllowsAMergedOutsideBlocker(t *testing.T) {
	t.Parallel()

	tickets := []ticketBlockedBy{
		{URL: "a", BlockedBy: []string{"outside"}},
	}
	ticketFeature := fixedFeature(map[string]string{"outside": "project:y"})
	got := closeUnderBlockedBy("project:x", tickets, ticketFeature, alwaysMerged)
	if got != nil {
		t.Fatalf("closeUnderBlockedBy = %+v, want nil: the outside blocker's pull request merged", got)
	}
}

func TestCloseUnderBlockedByRefusesABlockerNeverImported(t *testing.T) {
	t.Parallel()

	tickets := []ticketBlockedBy{
		{URL: "a", BlockedBy: []string{"unseen"}},
	}
	got := closeUnderBlockedBy("project:x", tickets, neverFeature, neverMerged)
	if got == nil {
		t.Fatal("closeUnderBlockedBy = nil, want a violation: an unseen blocker is outside the feature and unmerged")
		return
	}
	if got.BlockerFeature != "" {
		t.Errorf("BlockerFeature = %q, want empty for a blocker the app has never imported", got.BlockerFeature)
	}
}

func TestCloseUnderBlockedByReturnsTheFirstViolationOnly(t *testing.T) {
	t.Parallel()

	tickets := []ticketBlockedBy{
		{URL: "a", BlockedBy: []string{"outside1"}},
		{URL: "b", BlockedBy: []string{"outside2"}},
	}
	got := closeUnderBlockedBy("project:x", tickets, neverFeature, neverMerged)
	if got == nil || got.URL != "a" || got.Blocker != "outside1" {
		t.Errorf("closeUnderBlockedBy = %+v, want the first ticket's own violation", got)
	}
}

func neverFeature(string) string { return "" }
func neverMerged(string) bool    { return false }
func alwaysMerged(string) bool   { return true }

func fixedFeature(byURL map[string]string) func(string) string {
	return func(url string) string { return byURL[url] }
}
