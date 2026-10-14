package loop_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	storepkg "github.com/O-Marsters-1997/command-center/internal/store"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
)

func TestFollowUpSpawnsAFreshRunInTheExistingWorktreeWithTheTypedPrompt(t *testing.T) {
	t.Parallel()
	f := newPushFixture(t)
	if err := f.Store.QueueVerbIntentWithPayload(t.Context(), storepkg.VerbRequest{
		Ticket: f.Ticket.URL, Verb: plan.VerbFollowUp, Payload: "fix the flaky assertion in TestThing", At: testAt,
	}); err != nil {
		t.Fatalf("QueueVerbIntentWithPayload: %v", err)
	}
	fake := runner.NewFake()
	f.reset(testAt, fake)
	f.tick(t)

	if len(fake.Spawns) != 1 {
		t.Fatalf("spawns = %d, want 1", len(fake.Spawns))
	}
	spawned := fake.Spawns[0]
	if spawned.WorktreePath != f.Worktree {
		t.Errorf("follow-up spawned in %q, want the existing worktree %q", spawned.WorktreePath, f.Worktree)
	}
	if spawned.SystemPromptPath != "" {
		t.Errorf("system prompt path = %q, want none: the single-shot warning is implement-only", spawned.SystemPromptPath)
	}
	if !strings.Contains(spawned.Prompt, "cc/skills/follow-up/SKILL.md") {
		t.Errorf("prompt = %q, want it to reference the follow-up skill", spawned.Prompt)
	}
	if !strings.Contains(spawned.Prompt, "fix the flaky assertion in TestThing") {
		t.Errorf("prompt = %q, want it to carry the typed instruction", spawned.Prompt)
	}
	if strings.Contains(spawned.Prompt, "/implement") {
		t.Errorf("prompt = %q, want it never to compose the implement instruction", spawned.Prompt)
	}
	if strings.Contains(spawned.Prompt, "diff -u") || strings.HasPrefix(spawned.Prompt, "---") {
		t.Errorf("prompt = %q, want no re-run diff preamble", spawned.Prompt)
	}

	latest, err := f.Store.LatestRunsByTicket(t.Context())
	if err != nil {
		t.Fatalf("LatestRunsByTicket: %v", err)
	}
	summary, ok := latest[plan.TicketURL(f.Ticket.URL)]
	if !ok {
		t.Fatal("no run recorded after follow-up")
	}
	if summary.Kind != "follow_up" {
		t.Errorf("kind = %q, want follow_up", summary.Kind)
	}

	promptOnDisk, err := os.ReadFile(filepath.Join(f.ws.RunsDir, "1.prompt"))
	if err != nil {
		t.Fatalf("ReadFile prompt: %v", err)
	}
	if string(promptOnDisk) != spawned.Prompt {
		t.Errorf("stored prompt = %q, want it to match the spawned prompt exactly: %q", promptOnDisk, spawned.Prompt)
	}

	pending, err := f.Store.PendingVerbIntents(t.Context(), plan.VerbFollowUp)
	if err != nil {
		t.Fatalf("PendingVerbIntents: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("pending follow-up intents = %+v, want none: consumed", pending)
	}
}

func TestFollowUpRefusesWithNoWorktree(t *testing.T) {
	t.Parallel()
	f := newPushFixture(t)
	delete(f.Obs.Worktrees, branchRef("repo", "cc-1"))
	if err := f.Store.QueueVerbIntentWithPayload(t.Context(), storepkg.VerbRequest{
		Ticket: f.Ticket.URL, Verb: plan.VerbFollowUp, Payload: "do the thing", At: testAt,
	}); err != nil {
		t.Fatalf("QueueVerbIntentWithPayload: %v", err)
	}
	fake := runner.NewFake()
	f.reset(testAt, fake)
	f.tick(t)

	if len(fake.Spawns) != 0 {
		t.Fatalf("spawns = %d, want 0: follow-up never cuts a fresh worktree", len(fake.Spawns))
	}
	events := f.events(t, "follow_up_refused")
	if len(events) != 1 || events[0].TicketURL != f.Ticket.URL || !strings.Contains(events[0].Detail, "no worktree") {
		t.Errorf("events = %+v, want a follow_up_refused naming the missing worktree", events)
	}
}
