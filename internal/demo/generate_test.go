package demo_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/demo"
)

func TestGenerateIsDeterministic(t *testing.T) {
	t.Parallel()
	want := demo.Generate(42, 12)
	if diff := cmp.Diff(want, demo.Generate(42, 12)); diff != "" {
		t.Errorf("Generate(42, 12) differs between calls (-want +got):\n%s", diff)
	}
	if n := len(want.Tickets); n != 12 {
		t.Errorf("Generate(42, 12) has %d tickets, want 12", n)
	}
}
