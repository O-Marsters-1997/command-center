package web

import (
	"fmt"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

type destruction struct {
	effect    string
	riskLabel string
	risk      func(view.Row) string
}

var destructiveVerbs = map[string]destruction{
	plan.VerbKill: {
		effect:    "signals the process group of its live agent run, stopping it where it is",
		riskLabel: "pgid",
		risk:      func(r view.Row) string { return r.Pgid },
	},
	plan.VerbRemoveWorktree: {
		effect:    "closes its GitHub issue, deletes its worktree from disk, and drops it from the board",
		riskLabel: "worktree",
		risk:      func(r view.Row) string { return r.Worktree },
	},
}

func confirmation(verb string, row view.Row) string {
	d, ok := destructiveVerbs[verb]
	if !ok {
		return ""
	}
	risk := d.risk(row)
	if risk == "" {
		risk = "none recorded"
	}
	return fmt.Sprintf("%s on %s %s. It cannot be undone. %s at risk: %s", verb, row.URL, d.effect, d.riskLabel, risk)
}
