package web

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

func (s *Server) handleExpand(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	e := view.Expand{Path: q.Get("path"), Edge: view.ExpandEdge(q.Get("edge"))}
	var nums [3]int
	for i, key := range []string{"hunk", "above", "below"} {
		n, err := strconv.Atoi(q.Get(key))
		if err != nil {
			return errorf(http.StatusBadRequest, "%s %q is not a number", key, q.Get(key))
		}
		nums[i] = n
	}
	e.Hunk = nums[0]
	above, below := nums[1], nums[2]
	x, err := s.view.ExpandMore(r.Context(), s.clock.Now(), sessionRef(r), e, above, below)
	if err != nil {
		return fmt.Errorf("expand gap: %w", err)
	}
	return renderHTML(w, "expansion", x)
}

func (s *Server) handleChanges(w http.ResponseWriter, r *http.Request) error {
	page, err := s.view.Changes(r.Context(), s.clock.Now(), sessionRef(r))
	if err != nil {
		return fmt.Errorf("load changes: %w", err)
	}
	page.Sidebar = sidebarFrom(r)
	return renderHTML(w, "changes.tmpl", page)
}
