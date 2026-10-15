package verdict_test

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/verdict"
)

func TestUnknownKindHasNoWireName(t *testing.T) {
	t.Parallel()

	if got := verdict.KindUnknown.String(); got != "" {
		t.Errorf("KindUnknown.String() = %q, want empty", got)
	}
}
