package plan_test

import (
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestCompose(t *testing.T) {
	t.Parallel()

	t.Run("implements the ticket", func(t *testing.T) {
		t.Parallel()
		ticket := plan.Ticket{URL: "sandbox://CC-1"}
		if got, want := plan.Compose(ticket), "/implement sandbox://CC-1"; got != want {
			t.Errorf("Compose(%+v) = %q, want %q", ticket, got, want)
		}
	})

	t.Run("ends with the launch context", func(t *testing.T) {
		t.Parallel()
		ticket := plan.Ticket{
			URL: "sandbox://CC-2", WorkedExampleBranch: "cc-1-first", LaunchContext: "prefer table tests",
		}
		if got := plan.Compose(ticket); !strings.HasSuffix(got, "prefer table tests") {
			t.Errorf("Compose(%+v) = %q, want it to end with the launch context", ticket, got)
		}
	})

	t.Run("adds a worked example when the blocker branch is set", func(t *testing.T) {
		t.Parallel()
		ticket := plan.Ticket{URL: "sandbox://CC-2", WorkedExampleBranch: "cc-1-first"}

		got := plan.Compose(ticket)
		if !strings.HasPrefix(got, "/implement sandbox://CC-2") {
			t.Errorf("Compose(%+v) = %q, want the implement instruction to lead", ticket, got)
		}
		for _, want := range []string{"## Worked example: cc-1-first", "git diff main...cc-1-first"} {
			if !strings.Contains(got, want) {
				t.Errorf("Compose(%+v) = %q, want it to mention %q", ticket, got, want)
			}
		}
	})
}

func TestComposeFollowUp(t *testing.T) {
	t.Parallel()

	got := plan.ComposeFollowUp("fix the flaky assertion in TestThing")
	for _, want := range []string{"Do not push", "git log -5", "fix the flaky assertion in TestThing"} {
		if !strings.Contains(got, want) {
			t.Errorf("ComposeFollowUp(text) = %q, want it to mention %q", got, want)
		}
	}
	if strings.Contains(got, "SKILL.md") {
		t.Errorf("ComposeFollowUp = %q, want it to name no skill file", got)
	}
	if strings.Contains(got, "/implement") {
		t.Errorf("ComposeFollowUp(text) = %q, want no implement instruction", got)
	}
}

func TestComposeMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		after plan.Outcome
		want  string
	}{
		{after: plan.OutcomeContinued, want: "last tool call may have been cut off"},
		{after: plan.OutcomeAsked, want: "answer to your question"},
		{after: plan.OutcomeFailed, want: "follow-up instruction"},
	}
	for _, tt := range tests {
		t.Run(tt.after.String(), func(t *testing.T) {
			t.Parallel()
			got := plan.ComposeMessage("use sqlite", tt.after)
			if !strings.Contains(got, tt.want) || !strings.Contains(got, "use sqlite") {
				t.Errorf("ComposeMessage(text, %v) = %q, want %q and the text", tt.after, got, tt.want)
			}
		})
	}
}

func TestHash(t *testing.T) {
	t.Parallel()

	t.Run("is stable and non-empty", func(t *testing.T) {
		t.Parallel()
		first, again := plan.Hash("/implement sandbox://CC-1"), plan.Hash("/implement sandbox://CC-1")
		if first != again || first == "" {
			t.Errorf("Hash(same input) = %q then %q, want one non-empty value", first, again)
		}
	})

	t.Run("changes with the input", func(t *testing.T) {
		t.Parallel()
		if a, b := plan.Hash("/implement sandbox://CC-1"), plan.Hash("/implement sandbox://CC-2"); a == b {
			t.Errorf("Hash(CC-1) = Hash(CC-2) = %q, want them to differ", a)
		}
	})

	t.Run("changes when a worked example is composed in", func(t *testing.T) {
		t.Parallel()
		ticket := plan.Ticket{URL: "sandbox://CC-2"}
		without := plan.Hash(plan.Compose(ticket))
		ticket.WorkedExampleBranch = "cc-1-first"
		if with := plan.Hash(plan.Compose(ticket)); with == without {
			t.Errorf("Hash(Compose with worked example) = %q, want it to differ from %q", with, without)
		}
	})
}
