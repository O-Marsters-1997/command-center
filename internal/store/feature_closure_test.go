package store

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestCloseUnderBlockedBy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		tickets []ticketBlockedBy
		feature map[string]string
		merged  map[string]bool
		want    *FeatureClosureError
	}{
		{
			name:    "a blocker inside the feature",
			tickets: []ticketBlockedBy{{URL: "a", BlockedBy: []string{"b"}}, {URL: "b"}},
			feature: map[string]string{"b": "project:x"},
		},
		{
			name:    "a withdrawn unmerged blocker still tagged to the feature",
			tickets: []ticketBlockedBy{{URL: "a", BlockedBy: []string{"withdrawn"}}},
			feature: map[string]string{"withdrawn": "project:x"},
		},
		{
			name:    "a merged blocker outside the feature",
			tickets: []ticketBlockedBy{{URL: "a", BlockedBy: []string{"outside"}}},
			feature: map[string]string{"outside": "project:y"},
			merged:  map[string]bool{"outside": true},
		},
		{
			name:    "an unmerged blocker outside the feature",
			tickets: []ticketBlockedBy{{URL: "a", BlockedBy: []string{"outside"}}},
			feature: map[string]string{"outside": "project:y"},
			want:    &FeatureClosureError{Feature: "project:x", URL: "a", Blocker: "outside", BlockerFeature: "project:y"},
		},
		{
			name:    "a blocker never imported",
			tickets: []ticketBlockedBy{{URL: "a", BlockedBy: []string{"unseen"}}},
			want:    &FeatureClosureError{Feature: "project:x", URL: "a", Blocker: "unseen"},
		},
		{
			name: "only the first violation",
			tickets: []ticketBlockedBy{
				{URL: "a", BlockedBy: []string{"outside1"}},
				{URL: "b", BlockedBy: []string{"outside2"}},
			},
			want: &FeatureClosureError{Feature: "project:x", URL: "a", Blocker: "outside1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := closeUnderBlockedBy("project:x", tt.tickets, tt.feature, tt.merged)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("closeUnderBlockedBy(project:x) mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
