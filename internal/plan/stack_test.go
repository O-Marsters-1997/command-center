package plan_test

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestStackDepth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		branch       string
		baseByBranch map[string]string
		want         int
	}{
		{
			name:         "a branch with no recorded base is a root",
			branch:       "cc-1",
			baseByBranch: map[string]string{},
			want:         0,
		},
		{
			name:         "a branch based on main is a root",
			branch:       "cc-1",
			baseByBranch: map[string]string{"cc-1": "main"},
			want:         0,
		},
		{
			name:         "one hop off a root is depth one",
			branch:       "cc-2",
			baseByBranch: map[string]string{"cc-1": "main", "cc-2": "cc-1"},
			want:         1,
		},
		{
			name:   "a chain three deep counts every hop",
			branch: "cc-4",
			baseByBranch: map[string]string{
				"cc-1": "main", "cc-2": "cc-1", "cc-3": "cc-2", "cc-4": "cc-3",
			},
			want: 3,
		},
		{
			name:   "a fan-out sibling is depth one regardless of the other siblings",
			branch: "cc-5",
			baseByBranch: map[string]string{
				"cc-1": "main", "cc-2": "cc-1", "cc-3": "cc-1", "cc-4": "cc-1", "cc-5": "cc-1",
			},
			want: 1,
		},
		{
			name:         "a cycle terminates rather than looping forever",
			branch:       "cc-a",
			baseByBranch: map[string]string{"cc-a": "cc-b", "cc-b": "cc-a"},
			want:         1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := plan.StackDepth(tt.branch, tt.baseByBranch); got != tt.want {
				t.Errorf("StackDepth(%q) = %d, want %d", tt.branch, got, tt.want)
			}
		})
	}
}
