package plan_test

import (
	"strings"
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

func TestComposeResolve(t *testing.T) {
	t.Parallel()

	ticket := plan.Ticket{URL: "sandbox://CC-1", Branch: "cc-1"}

	got := plan.ComposeResolve(ticket)
	for _, want := range []string{
		"cc/skills/resolve-merge-conflict/SKILL.md", "origin/main", "cc-1", "do not commit", "do not push",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("ComposeResolve = %q, want it to mention %q", got, want)
		}
	}
	if got == plan.Compose(ticket) {
		t.Error("ComposeResolve must not compose the same prompt as a launch")
	}
}

func TestComposeFollowUp(t *testing.T) {
	t.Parallel()

	got := plan.ComposeFollowUp("fix the flaky assertion in TestThing", "")
	for _, want := range []string{"cc/skills/follow-up/SKILL.md", "fix the flaky assertion in TestThing"} {
		if !strings.Contains(got, want) {
			t.Errorf("ComposeFollowUp = %q, want it to mention %q", got, want)
		}
	}
	if strings.Contains(got, "/implement") {
		t.Errorf("ComposeFollowUp = %q, want it never to compose the implement instruction", got)
	}
}

func TestComposeFollowUpAppendsTheCISectionWhenGiven(t *testing.T) {
	t.Parallel()

	got := plan.ComposeFollowUp("fix it", "## Failed CI log\n\nsome log")
	if !strings.Contains(got, "## Failed CI log\n\nsome log") {
		t.Errorf("ComposeFollowUp = %q, want it to carry the CI section", got)
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
