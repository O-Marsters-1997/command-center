package web

import (
	"fmt"
	"net/http"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/web/hx"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) error {
	ref := view.SessionRef{Owner: r.PathValue("owner"), Name: r.PathValue("name"), N: r.PathValue("n")}
	page, err := s.view.Session(r.Context(), s.clock.Now(), ref, r.URL.Query().Get("log") == "raw")
	if err != nil {
		return fmt.Errorf("load session: %w", err)
	}
	return renderHTML(w, "session.tmpl", page)
}

func (s *Server) handleSessionActions(w http.ResponseWriter, r *http.Request) error {
	ref := view.SessionRef{Owner: r.PathValue("owner"), Name: r.PathValue("name"), N: r.PathValue("n")}
	page, err := s.view.Session(r.Context(), s.clock.Now(), ref, false)
	if err != nil {
		return fmt.Errorf("load session: %w", err)
	}
	return renderFragment(w, r, page.TranscriptPath, "sessionActions", page)
}

func (s *Server) afterSessionVerb(w http.ResponseWriter, r *http.Request, ticketURL string, verb plan.Verb) error {
	ref, err := view.ParseSessionRef(plan.TicketURL(ticketURL))
	if err != nil {
		return errorf(http.StatusBadRequest, "%q has no session page", ticketURL)
	}
	if !hx.IsRequest(r) {
		seeOther(w, r, ref.Path())
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
