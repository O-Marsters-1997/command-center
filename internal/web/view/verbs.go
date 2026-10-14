package view

import (
	"fmt"
	"slices"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

type VerbButton struct {
	Verb    plan.Verb
	Label   string
	Key     string
	Confirm string
	Pending bool
}

var verbKeys = map[plan.Verb]string{
	plan.VerbReRun:          "r",
	plan.VerbRemoveWorktree: "e",
}

func (r Row) VerbButtons() []VerbButton {
	var buttons []VerbButton
	for _, verb := range r.Verbs {
		if verb == plan.VerbLaunch || verb == plan.VerbFollowUp {
			continue
		}
		button := VerbButton{
			Verb: verb, Label: string(verb), Key: verbKeys[verb], Confirm: confirmation(verb, r),
			Pending: slices.Contains(r.PendingVerbs, verb),
		}
		if verb == plan.VerbCancel {
			button.Label = fmt.Sprintf("cancel launch (%d tickets)", r.CancelCount)
		}
		buttons = append(buttons, button)
	}
	return buttons
}

func confirmation(verb plan.Verb, row Row) string {
	if verb != plan.VerbKill && verb != plan.VerbRemoveWorktree {
		return ""
	}
	effect, riskLabel, risk := "signals the process group of its live agent run, stopping it where it is", "pgid", row.Pgid
	if verb == plan.VerbRemoveWorktree {
		effect = "closes its GitHub issue, deletes its worktree from disk, and drops it from the board"
		riskLabel, risk = "worktree", row.Worktree
	}
	if risk == "" {
		risk = "none recorded"
	}
	return fmt.Sprintf("%s on %s %s. It cannot be undone. %s at risk: %s", verb, row.URL, effect, riskLabel, risk)
}
