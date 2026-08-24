package plan_test

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestStackedReadyToMergeWarning(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		baseRef string
		labels  []string
		want    bool
	}{
		{
			name:    "a main-based PR carrying the label warns of nothing",
			baseRef: "main",
			labels:  []string{"ready-to-merge"},
			want:    false,
		},
		{
			name:    "a stacked PR without the label warns of nothing",
			baseRef: "cc-1",
			labels:  []string{"keep-open"},
			want:    false,
		},
		{
			name:    "a stacked PR carrying the label is invariant 2's hazard",
			baseRef: "cc-1",
			labels:  []string{"keep-open", "ready-to-merge"},
			want:    true,
		},
		{
			name:    "no recorded base at all warns of nothing",
			baseRef: "",
			labels:  []string{"ready-to-merge"},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := plan.StackedReadyToMergeWarning(tt.baseRef, tt.labels); got != tt.want {
				t.Errorf("StackedReadyToMergeWarning(%q, %v) = %v, want %v", tt.baseRef, tt.labels, got, tt.want)
			}
		})
	}
}
