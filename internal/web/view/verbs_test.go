package view_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

func TestKeymap_SessionVerbKeysMatchButtons(t *testing.T) {
	t.Parallel()

	row := view.Row{Verbs: []plan.Verb{plan.VerbReRun, plan.VerbRemoveWorktree}}
	var session []string
	for _, group := range view.Keymap {
		if group.Title != "In a session" {
			continue
		}
		for _, keyRow := range group.Rows {
			session = append(session, keyRow.Keys)
		}
	}
	for _, button := range row.VerbButtons() {
		if want := strings.ToUpper(button.Key); button.Key == "" || !slices.Contains(session, want) {
			t.Errorf("session keymap rows = %q, want one for %s key %q", session, button.Verb, want)
		}
	}
}

func TestRow_VerbButtons(t *testing.T) {
	t.Parallel()

	row := view.Row{
		URL: "sandbox://CC-1", Pgid: "123", Worktree: "/repos/cc-1", CancelCount: 3,
		Verbs: []plan.Verb{
			plan.VerbLaunch, plan.VerbFollowUp, plan.VerbCancel,
			plan.VerbKill, plan.VerbReRun, plan.VerbRemoveWorktree,
		},
		PendingVerbs: []plan.Verb{plan.VerbReRun},
	}
	want := []view.VerbButton{
		{Verb: plan.VerbCancel, Label: "cancel launch (3 tickets)"},
		{Verb: plan.VerbKill, Label: "kill", Confirm: "kill on sandbox://CC-1 signals the process group of " +
			"its live agent run, stopping it where it is. It cannot be undone. pgid at risk: 123"},
		{Verb: plan.VerbReRun, Label: "re-run", Key: "r", Pending: true},
		{Verb: plan.VerbRemoveWorktree, Label: "remove-worktree", Key: "e", Confirm: "remove-worktree on sandbox://CC-1 " +
			"closes its GitHub issue, deletes its worktree from disk, and drops it from the board. " +
			"It cannot be undone. worktree at risk: /repos/cc-1"},
	}
	if diff := cmp.Diff(want, row.VerbButtons()); diff != "" {
		t.Errorf("VerbButtons() mismatch (-want +got):\n%s", diff)
	}
}
