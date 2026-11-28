package memory_test

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan/memory"
)

func TestRunPeak_Heavy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		peak memory.RunPeak
		want bool
	}{
		{"below ninety percent", memory.RunPeak{RSS: 89, Total: 100}, false},
		{"exactly ninety percent", memory.RunPeak{RSS: 90, Total: 100}, true},
		{"unknown total", memory.RunPeak{RSS: 90}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.peak.Heavy(); got != tt.want {
				t.Errorf("Heavy() = %v, want %v", got, tt.want)
			}
		})
	}
}
