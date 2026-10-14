package tp_test

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/tp"
)

func TestRemoveRejectsAnUnknownMode(t *testing.T) {
	t.Parallel()

	if err := tp.Remove(t.Context(), t.TempDir(), "cc-1", tp.RemoveUnknown); err == nil {
		t.Error("Remove(RemoveUnknown) = nil error, want one")
	}
}
