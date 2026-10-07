package web

import "net/http"

func (s *Server) handleInsightsPage(w http.ResponseWriter, r *http.Request) error {
	data, err := s.view.InsightsPage(r.Context(), s.clock.Now(), r.URL.Query())
	if err != nil {
		return err
	}
	return renderHTML(w, "insights.tmpl", data)
}

func (s *Server) handleInsights(w http.ResponseWriter, r *http.Request) error {
	resp, err := s.view.Insights(r.Context(), s.clock.Now(), r.URL.Query())
	if err != nil {
		return err
	}
	return writeJSON(w, resp)
}
