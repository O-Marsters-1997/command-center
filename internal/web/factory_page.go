package web

import (
	"fmt"
	"net/http"

	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

func (s *Server) handleFactory(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	params := view.FactoryParams{
		Params:     view.ParseParams(q),
		BlockedAll: q.Get("blocked") == "all",
		By:         view.LaneBy(q.Get("by")),
		Closed:     q["closed"],
	}
	page, err := s.view.Factory(r.Context(), s.now(), params)
	if err != nil {
		return fmt.Errorf("load factory: %w", err)
	}
	page.Sidebar = sidebarFrom(r)
	return renderHTML(w, "factory.tmpl", page)
}
