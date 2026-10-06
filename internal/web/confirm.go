package web

import (
	_ "embed"
	"fmt"
	"html/template"
	"net/http"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

//go:embed confirm.tmpl
var confirmSource string

var confirmPage = template.Must(page.New("confirm").Parse(confirmSource))

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

type confirmView struct {
	view.Chrome
	URL       string
	Verb      string
	Effect    string
	RiskLabel string
	Risk      string
}

func (s *Server) handleConfirm(w http.ResponseWriter, r *http.Request) {
	verb := r.URL.Query().Get("verb")
	ticketURL := r.URL.Query().Get("ticket")
	if verb == "" || ticketURL == "" {
		http.Error(w, "verb and ticket are both required", http.StatusBadRequest)
		return
	}
	destroys, ok := destructiveVerbs[verb]
	if !ok {
		http.Error(w, fmt.Sprintf("verb %q needs no confirmation", verb), http.StatusBadRequest)
		return
	}

	board, err := s.view.Board(r.Context(), s.clock.Now(), view.ParseParams(nil))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	target, ok := board.Row(ticketURL)
	if !ok {
		http.Error(w, fmt.Sprintf("unknown ticket %q", ticketURL), http.StatusBadRequest)
		return
	}

	confirm := confirmView{
		Chrome:    board.Chrome,
		URL:       target.URL,
		Verb:      verb,
		Effect:    destroys.effect,
		RiskLabel: destroys.riskLabel,
		Risk:      destroys.risk(target),
	}
	renderHTML(w, confirmPage, confirm)
}
