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
	URL       string
	Verb      string
	Effect    string
	RiskLabel string
	Risk      string
}

func (s *Server) handleConfirm(w http.ResponseWriter, r *http.Request) {
	verb := r.URL.Query().Get("verb")
	ticketURL := r.URL.Query().Get("task")
	if verb == "" || ticketURL == "" {
		http.Error(w, "verb and ticket are both required", http.StatusBadRequest)
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
	target, ok := findRow(view, ticketURL)
	if !ok {
		http.Error(w, fmt.Sprintf("unknown ticket %q", ticketURL), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	confirm := confirmView{
		URL:       target.URL,
		Verb:      verb,
		Effect:    destroys.effect,
		RiskLabel: destroys.riskLabel,
		Risk:      destroys.risk(target),
	}
	if err := confirmPage.Execute(w, confirm); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func findRow(view pageView, ticketURL string) (row, bool) {
	for _, g := range view.Groups {
		if g.Root != nil && g.Root.URL == ticketURL {
			return *g.Root, true
		}
		for _, child := range g.Children {
			if child.URL == ticketURL {
				return child, true
			}
		}
	}
	return row{}, false
}
