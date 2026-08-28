package plan_test

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestCompose(t *testing.T) {
	t.Parallel()

	ticket := plan.Ticket{URL: "sandbox://CC-1"}

	got := plan.Compose(ticket)
	want := "/implement sandbox://CC-1"
	if got != want {
		t.Errorf("Compose = %q, want %q", got, want)
	}
}

func TestHashIsStableAndSensitiveToInput(t *testing.T) {
	t.Parallel()

	first := plan.Hash("/implement sandbox://CC-1")
	again := plan.Hash("/implement sandbox://CC-1")
	if first != again {
		t.Errorf("Hash is not stable: %q != %q", first, again)
	}
	if first == "" {
		t.Error("Hash returned an empty string")
	}

	edited := plan.Hash("/implement sandbox://CC-2")
	if edited == first {
		t.Error("editing the composed input did not change the hash")
	}
}
