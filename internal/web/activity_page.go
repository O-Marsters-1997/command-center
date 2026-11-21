package web

import (
	"fmt"
	"net/http"
)

func (s *Server) handleActivity(w http.ResponseWriter, r *http.Request) error {
	page, err := s.view.ActivityPage(r.Context(), s.now(), r.URL.Query())
	if err != nil {
		return fmt.Errorf("load activity page: %w", err)
	}
	page.Sidebar = sidebarFrom(r)
	return renderHTML(w, "activity.tmpl", page)
}
