package plan_test

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestAllGenerated(t *testing.T) {
	t.Parallel()

	policy := plan.GeneratedPolicy{
		Paths:        []string{"internal/cc/assets/dist/**", "internal/cc/testdata/*.golden.html"},
		BuildCommand: []string{"just", "assets"},
	}

	tests := []struct {
		name      string
		conflicts []string
		want      bool
	}{
		{
			name:      "a single generated path under the dir/** entry",
			conflicts: []string{"internal/cc/assets/dist/app.css"},
			want:      true,
		},
		{
			name:      "a leaf glob match",
			conflicts: []string{"internal/cc/testdata/board.golden.html"},
			want:      true,
		},
		{
			name:      "every conflicted path generated",
			conflicts: []string{"internal/cc/assets/dist/app.css", "internal/cc/testdata/board.golden.html"},
			want:      true,
		},
		{
			name:      "one hand-written path among generated ones refuses the whole set",
			conflicts: []string{"internal/cc/assets/dist/app.css", "internal/cc/push.go"},
			want:      false,
		},
		{
			name:      "no conflicted paths is not this gate's to resolve",
			conflicts: nil,
			want:      false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := plan.AllGenerated(tt.conflicts, policy); got != tt.want {
				t.Errorf("AllGenerated(%v) = %v, want %v", tt.conflicts, got, tt.want)
			}
		})
	}
}

func TestAllGeneratedRefusesARepoThatNamedNoBuildCommand(t *testing.T) {
	t.Parallel()

	policy := plan.GeneratedPolicy{Paths: []string{"internal/cc/assets/dist/**"}}
	conflicts := []string{"internal/cc/assets/dist/app.css"}

	if got := plan.AllGenerated(conflicts, policy); got {
		t.Errorf("AllGenerated(%v) = %v, want false: no build command means the repo opted out", conflicts, got)
	}
}
