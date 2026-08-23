package cc

import (
	"strings"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

// RenderStatesPage renders the page from one synthetic row per given state. It is the only way
// to golden the verbs of every state at once: reaching all fourteen through a real store would
// need runs, pushes and PR snapshots that no single fixture can hold simultaneously.
func RenderStatesPage(states []plan.State) (string, error) {
	view := pageView{ObserveAge: "0s ago", LaunchVerb: plan.VerbLaunch, CancelVerb: plan.VerbCancel}
	for _, state := range states {
		view.Rows = append(view.Rows, row{
			TicketURL: "sandbox://" + state.String(),
			State:     state.String(),
			Verbs:     plan.Verbs(state),
		})
	}

	var out strings.Builder
	if err := page.Execute(&out, view); err != nil {
		return "", err
	}
	return out.String(), nil
}
