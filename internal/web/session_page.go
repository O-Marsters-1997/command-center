package web

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) error {
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil {
		return withStatus(http.StatusNotFound, view.ErrSessionNotFound)
	}
	ref := view.SessionRef{Owner: r.PathValue("owner"), Name: r.PathValue("name"), N: n}
	page, err := s.view.Session(r.Context(), s.clock.Now(), ref, r.URL.Query().Get("log") == "raw")
	if err != nil {
		return fmt.Errorf("load session: %w", err)
	}
	if r.URL.Query().Get("part") == "actions" {
		return renderHTML(w, "sessionActions", page)
	}
	return renderHTML(w, "session.tmpl", page)
}

func (s *Server) afterSessionVerb(w http.ResponseWriter, r *http.Request, ticketURL string, verb plan.Verb) error {
	ref, err := view.ParseSessionRef(plan.TicketURL(ticketURL))
	if err != nil {
		return errorf(http.StatusBadRequest, "%q has no session page", ticketURL)
	}
	if r.Header.Get("HX-Request") == "" {
		http.Redirect(w, r, ref.Path(), http.StatusSeeOther)
		return nil
	}
	page, err := s.view.Session(r.Context(), s.clock.Now(), ref, false)
	if err != nil {
		return fmt.Errorf("load session: %w", err)
	}
	page.Toast = "Queued " + string(verb)
	page.ResetComposer = verb == plan.VerbFollowUp
	return renderHTML(w, "sessionActions", page)
}
