package web

import (
	"fmt"
	"net/http"
)

func (s *Server) handleChanges(w http.ResponseWriter, r *http.Request) error {
	page, err := s.view.Changes(r.Context(), s.clock.Now(), sessionRef(r))
	if err != nil {
		return fmt.Errorf("load changes: %w", err)
	}
	page.Sidebar = sidebarFrom(r)
	return renderHTML(w, "changes.tmpl", page)
}
