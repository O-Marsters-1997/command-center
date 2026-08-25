package cc

import (
	_ "embed"
	"fmt"
	"html/template"
	"net/http"
)

//go:embed confirm.tmpl
var confirmSource string

var confirmPage = template.Must(page.New("confirm").Parse(confirmSource))

type destruction struct {
	effect    string
	riskLabel string
	risk      func(row) string
}

var destructiveVerbs = map[string]destruction{
	killVerb: {
		effect:    "signals the process group of its live agent run, stopping it where it is",
		riskLabel: "pgid",
		risk:      func(r row) string { return r.Pgid },
	},
	removeWorktreeVerb: {
		effect:    "deletes its worktree from disk",
		riskLabel: "worktree",
		risk:      func(r row) string { return r.Worktree },
	},
}

type confirmView struct {
	TicketURL string
	Verb      string
	Effect    string
	RiskLabel string
	Risk      string
}

func (s *Server) handleConfirm(w http.ResponseWriter, r *http.Request) {
	verb := r.URL.Query().Get("verb")
	taskURL := r.URL.Query().Get("task")
	if verb == "" || taskURL == "" {
		http.Error(w, "verb and task are both required", http.StatusBadRequest)
		return
	}
	destroys, ok := destructiveVerbs[verb]
	if !ok {
		http.Error(w, fmt.Sprintf("verb %q needs no confirmation", verb), http.StatusBadRequest)
		return
	}

	view, err := s.render(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	target, ok := findRow(view, taskURL)
	if !ok {
		http.Error(w, fmt.Sprintf("unknown task %q", taskURL), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	confirm := confirmView{
		TicketURL: target.TicketURL,
		Verb:      verb,
		Effect:    destroys.effect,
		RiskLabel: destroys.riskLabel,
		Risk:      destroys.risk(target),
	}
	if err := confirmPage.Execute(w, confirm); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func findRow(view pageView, taskURL string) (row, bool) {
	for _, g := range view.Groups {
		if g.Root != nil && g.Root.TicketURL == taskURL {
			return *g.Root, true
		}
		for _, child := range g.Children {
			if child.TicketURL == taskURL {
				return child, true
			}
		}
	}
	return row{}, false
}
