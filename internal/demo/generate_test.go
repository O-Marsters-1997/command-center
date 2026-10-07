package demo_test

import (
	"fmt"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/demo"
)

func TestGenerateIsDeterministic(t *testing.T) {
	want := fmt.Sprintf("%+v", demo.Generate(42, 12))
	if got := fmt.Sprintf("%+v", demo.Generate(42, 12)); got != want {
		t.Errorf("Generate(42, 12) differs between calls:\n got %s\nwant %s", got, want)
	}
	if n := len(demo.Generate(42, 12).Tickets); n != 12 {
		t.Errorf("Generate(42, 12) has %d tickets, want 12", n)
	}
}
