package cc

import (
	"strings"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

// RenderStatesPage renders the page from one synthetic row per given state. It is the only way
// to golden the verbs of every state at once: reaching every one through a real store would
// need runs, pushes and PR snapshots that no single fixture can hold simultaneously.
func RenderStatesPage(states []plan.State) (string, error) {
	view := pageView{ObserveAge: "0s ago"}
	for _, state := range states {
		r := row{
			TicketURL: "sandbox://" + state.String(),
			State:     state.String(),
			Verbs:     plan.Verbs(state),
		}
		view.Groups = append(view.Groups, group{Children: []row{r}})
	}

	var out strings.Builder
	if err := page.Execute(&out, view); err != nil {
		return "", err
	}
	return out.String(), nil
}

// TailLog exposes the log tail to the external test package: driving it through the route needs
// a store and a run, and the window branch only shows on a file larger than the window.
var TailLog = tailLog
