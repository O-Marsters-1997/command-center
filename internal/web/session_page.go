package web

import (
	"errors"
	"net/http"

	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) error {
	page, err := s.view.Session(
		r.Context(), s.clock.Now(),
		r.PathValue("owner"), r.PathValue("name"), r.PathValue("n"),
		r.URL.Query().Get("log") == "raw",
	)
	if errors.Is(err, view.ErrSessionNotFound) {
		return withStatus(http.StatusNotFound, err)
	}
	if err != nil {
		return err
	}
	return renderHTML(w, "session.tmpl", page)
}
