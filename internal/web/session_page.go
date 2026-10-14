package web

import (
	"net/http"
	"strings"

	"github.com/O-Marsters-1997/command-center/internal/web/hx"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) error {
	page, err := s.view.Session(
		r.Context(), s.clock.Now(),
		r.PathValue("owner"), r.PathValue("name"), r.PathValue("n"),
		r.URL.Query().Get("log") == "raw",
	)
	if err != nil {
		return err
	}
	return renderHTML(w, "session.tmpl", page)
}

func (s *Server) handleSessionActions(w http.ResponseWriter, r *http.Request) error {
	page, err := s.view.Session(
		r.Context(), s.clock.Now(),
		r.PathValue("owner"), r.PathValue("name"), r.PathValue("n"),
		false,
	)
	if err != nil {
		return err
	}
	return renderFragment(w, r, page.TranscriptPath, "sessionActions", page)
}

func (s *Server) afterSessionVerb(w http.ResponseWriter, r *http.Request, ticketURL, verb string) error {
	self := view.SessionPath(ticketURL)
	if self == "" {
		return errorf(http.StatusBadRequest, "%q has no session page", ticketURL)
	}
	if !hx.IsRequest(r) {
		seeOther(w, r, self)
		return nil
	}
	parts := strings.Split(strings.TrimPrefix(self, "/s/"), "/")
	page, err := s.view.Session(r.Context(), s.clock.Now(), parts[0], parts[1], parts[2], false)
	if err != nil {
		return err
	}
	page.Toast = "Queued " + verb
	page.ResetComposer = verb == "follow-up"
	return renderHTML(w, "sessionActions", page)
}
