package view

import (
	"fmt"
	"slices"
	"strings"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

type VerbButton struct {
	Verb    plan.Verb
	Label   string
	Key     string
	Confirm string
	Pending bool
}

type verbPresentation struct {
	label   func(Row) string
	key     string
	confirm func(Row) string
}

var verbTable = map[plan.Verb]verbPresentation{
	plan.VerbCancel: {
		label: func(r Row) string { return fmt.Sprintf("cancel launch (%d tickets)", r.CancelCount) },
	},
	plan.VerbKill: {
		confirm: func(r Row) string {
			return confirmation(plan.VerbKill, r, "signals the process group of its live agent run, stopping it where it is", "pgid", r.Pgid)
		},
	},
	plan.VerbReRun: {key: "r"},
	plan.VerbRemoveWorktree: {
		key: "e",
		confirm: func(r Row) string {
			return confirmation(plan.VerbRemoveWorktree, r,
				"closes its GitHub issue, deletes its worktree from disk, and drops it from the board", "worktree", r.Worktree)
		},
	},
}

func (r Row) VerbButtons() []VerbButton {
	var buttons []VerbButton
	for _, verb := range r.Verbs {
		if verb == plan.VerbLaunch || verb == plan.VerbFollowUp {
			continue
		}
		spec := verbTable[verb]
		button := VerbButton{
			Verb: verb, Label: string(verb), Key: spec.key,
			Pending: slices.Contains(r.PendingVerbs, verb),
		}
		if spec.label != nil {
			button.Label = spec.label(r)
		}
		if spec.confirm != nil {
			button.Confirm = spec.confirm(r)
		}
		buttons = append(buttons, button)
	}
	return buttons
}

func confirmation(verb plan.Verb, row Row, effect, riskLabel, risk string) string {
	if risk == "" {
		risk = "none recorded"
	}
	return fmt.Sprintf("%s on %s %s. It cannot be undone. %s at risk: %s", verb, row.URL, effect, riskLabel, risk)
}

func verbKeyRow(verb plan.Verb, does string) KeyRow {
	return KeyRow{Keys: strings.ToUpper(verbTable[verb].key), Does: does}
}
