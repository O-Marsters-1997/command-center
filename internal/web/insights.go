package web

import (
	"fmt"
	"net/http"
)

func (s *Server) handleInsightsPage(w http.ResponseWriter, r *http.Request) error {
	data, err := s.view.InsightsPage(r.Context(), s.now(), r.URL.Query())
	if err != nil {
		return fmt.Errorf("load insights page: %w", err)
	}
	return renderHTML(w, "insights.tmpl", data)
}

func (s *Server) handleInsights(w http.ResponseWriter, r *http.Request) error {
	resp, err := s.view.Insights(r.Context(), s.now(), r.URL.Query())
	if err != nil {
		return fmt.Errorf("load insights: %w", err)
	}
	return writeJSON(w, resp)
}
