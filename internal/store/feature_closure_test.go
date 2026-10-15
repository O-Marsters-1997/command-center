package store

import (
	"errors"
	"testing"
)

func TestCloseUnderBlockedByAllowsABlockerInsideTheFeature(t *testing.T) {
	t.Parallel()

	tickets := []ticketBlockedBy{
		{URL: "a", BlockedBy: []string{"b"}},
		{URL: "b"},
	}
	ticketFeature := fixedFeature(map[string]string{"b": "project:x"})
	got, err := closeUnderBlockedBy("project:x", tickets, ticketFeature, neverMerged)
	if err != nil || got != nil {
		t.Fatalf("closeUnderBlockedBy = %+v, %v, want nil, nil", got, err)
	}
}

func TestCloseUnderBlockedByAllowsAWithdrawnUnmergedBlockerStillTaggedToTheFeature(t *testing.T) {
	t.Parallel()

	tickets := []ticketBlockedBy{{URL: "a", BlockedBy: []string{"withdrawn"}}}
	ticketFeature := fixedFeature(map[string]string{"withdrawn": "project:x"})
	got, err := closeUnderBlockedBy("project:x", tickets, ticketFeature, neverMerged)
	if err != nil || got != nil {
		t.Fatalf("closeUnderBlockedBy = %+v, %v, want nil, nil: the blocker is still tagged to project:x", got, err)
	}
}

func TestCloseUnderBlockedByRefusesABlockerOutsideTheFeature(t *testing.T) {
	t.Parallel()

	tickets := []ticketBlockedBy{
		{URL: "a", BlockedBy: []string{"outside"}},
	}
	ticketFeature := fixedFeature(map[string]string{"outside": "project:y"})
	got, err := closeUnderBlockedBy("project:x", tickets, ticketFeature, neverMerged)
	if err != nil {
		t.Fatalf("closeUnderBlockedBy(project:x): %v", err)
	}
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
	got, err := closeUnderBlockedBy("project:x", tickets, ticketFeature, alwaysMerged)
	if err != nil || got != nil {
		t.Fatalf("closeUnderBlockedBy = %+v, %v, want nil, nil: the outside blocker's pull request merged", got, err)
	}
}

func TestCloseUnderBlockedByRefusesABlockerNeverImported(t *testing.T) {
	t.Parallel()

	tickets := []ticketBlockedBy{
		{URL: "a", BlockedBy: []string{"unseen"}},
	}
	got, err := closeUnderBlockedBy("project:x", tickets, neverFeature, neverMerged)
	if err != nil {
		t.Fatalf("closeUnderBlockedBy(project:x): %v", err)
	}
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
	got, err := closeUnderBlockedBy("project:x", tickets, neverFeature, neverMerged)
	if err != nil || got == nil || got.URL != "a" || got.Blocker != "outside1" {
		t.Errorf("closeUnderBlockedBy = %+v, %v, want the first ticket's own violation", got, err)
	}
}

func TestCloseUnderBlockedByPropagatesALookupError(t *testing.T) {
	t.Parallel()

	errLookup := errors.New("connection reset")
	tickets := []ticketBlockedBy{{URL: "a", BlockedBy: []string{"outside"}}}
	failingFeature := func(string) (string, error) { return "", errLookup }
	failingMerged := func(string) (bool, error) { return false, errLookup }

	if _, err := closeUnderBlockedBy("project:x", tickets, failingFeature, neverMerged); !errors.Is(err, errLookup) {
		t.Errorf("feature lookup error = %v, want %v", err, errLookup)
	}
	if _, err := closeUnderBlockedBy("project:x", tickets, neverFeature, failingMerged); !errors.Is(err, errLookup) {
		t.Errorf("merged lookup error = %v, want %v", err, errLookup)
	}
}

func neverFeature(string) (string, error) { return "", nil }
func neverMerged(string) (bool, error)    { return false, nil }
func alwaysMerged(string) (bool, error)   { return true, nil }

func fixedFeature(byURL map[string]string) func(string) (string, error) {
	return func(url string) (string, error) { return byURL[url], nil }
}
