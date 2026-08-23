package plan_test

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestCompose(t *testing.T) {
	t.Parallel()

	task := plan.Task{TicketURL: "sandbox://CC-1"}

	got := plan.Compose(task, nil)
	want := "/implement sandbox://CC-1"
	if got != want {
		t.Errorf("Compose = %q, want %q", got, want)
	}
}

func TestComposeAppendsSeamLines(t *testing.T) {
	t.Parallel()

	task := plan.Task{TicketURL: "sandbox://CC-1"}
	got := plan.Compose(task, []string{"seam one", "seam two"})
	want := "/implement sandbox://CC-1\nseam one\nseam two"
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

	edited := plan.Hash("/implement sandbox://CC-1\nnew seam content")
	if edited == first {
		t.Error("editing the composed input did not change the hash")
	}
}
