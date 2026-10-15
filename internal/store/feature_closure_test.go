package store

import (
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func neverFeature(string) (string, error) { return "", nil }
func neverMerged(string) (bool, error)    { return false, nil }
func alwaysMerged(string) (bool, error)   { return true, nil }

func fixedFeature(byURL map[string]string) func(string) (string, error) {
	return func(url string) (string, error) { return byURL[url], nil }
}

func TestCloseUnderBlockedBy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		tickets       []ticketBlockedBy
		ticketFeature func(string) (string, error)
		merged        func(string) (bool, error)
		want          *FeatureClosureError
	}{
		{
			name:          "a blocker inside the feature",
			tickets:       []ticketBlockedBy{{URL: "a", BlockedBy: []string{"b"}}, {URL: "b"}},
			ticketFeature: fixedFeature(map[string]string{"b": "project:x"}),
			merged:        neverMerged,
		},
		{
			name:          "a withdrawn unmerged blocker still tagged to the feature",
			tickets:       []ticketBlockedBy{{URL: "a", BlockedBy: []string{"withdrawn"}}},
			ticketFeature: fixedFeature(map[string]string{"withdrawn": "project:x"}),
			merged:        neverMerged,
		},
		{
			name:          "a merged blocker outside the feature",
			tickets:       []ticketBlockedBy{{URL: "a", BlockedBy: []string{"outside"}}},
			ticketFeature: fixedFeature(map[string]string{"outside": "project:y"}),
			merged:        alwaysMerged,
		},
		{
			name:          "an unmerged blocker outside the feature",
			tickets:       []ticketBlockedBy{{URL: "a", BlockedBy: []string{"outside"}}},
			ticketFeature: fixedFeature(map[string]string{"outside": "project:y"}),
			merged:        neverMerged,
			want:          &FeatureClosureError{Feature: "project:x", URL: "a", Blocker: "outside", BlockerFeature: "project:y"},
		},
		{
			name:          "a blocker never imported",
			tickets:       []ticketBlockedBy{{URL: "a", BlockedBy: []string{"unseen"}}},
			ticketFeature: neverFeature,
			merged:        neverMerged,
			want:          &FeatureClosureError{Feature: "project:x", URL: "a", Blocker: "unseen"},
		},
		{
			name: "only the first violation",
			tickets: []ticketBlockedBy{
				{URL: "a", BlockedBy: []string{"outside1"}},
				{URL: "b", BlockedBy: []string{"outside2"}},
			},
			ticketFeature: neverFeature,
			merged:        neverMerged,
			want:          &FeatureClosureError{Feature: "project:x", URL: "a", Blocker: "outside1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := closeUnderBlockedBy("project:x", tt.tickets, tt.ticketFeature, tt.merged)
			if err != nil {
				t.Fatalf("closeUnderBlockedBy(project:x) error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("closeUnderBlockedBy(project:x) mismatch (-want +got):\n%s", diff)
			}
		})
	}

	t.Run("errors", func(t *testing.T) {
		t.Parallel()
		errLookup := errors.New("connection reset")
		tests := []struct {
			name          string
			ticketFeature func(string) (string, error)
			merged        func(string) (bool, error)
		}{
			{name: "feature lookup", ticketFeature: func(string) (string, error) { return "", errLookup }, merged: neverMerged},
			{name: "merged lookup", ticketFeature: neverFeature, merged: func(string) (bool, error) { return false, errLookup }},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				tickets := []ticketBlockedBy{{URL: "a", BlockedBy: []string{"outside"}}}
				if _, err := closeUnderBlockedBy("project:x", tickets, tt.ticketFeature, tt.merged); !errors.Is(err, errLookup) {
					t.Errorf("closeUnderBlockedBy(%s fails) error = %v, want %v", tt.name, err, errLookup)
				}
			})
		}
	})
}
