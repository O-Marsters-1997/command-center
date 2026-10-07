package web

import (
	_ "embed"
	"html/template"
	"net/http"

	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

//go:embed insights.tmpl
var insightsPageSource string

var insightsPage = template.Must(page.New("insights").Parse(insightsPageSource))

func (s *Server) handleInsightsPage(w http.ResponseWriter, r *http.Request) {
	data, err := s.view.InsightsPage(r.Context(), s.clock.Now(), view.ParseParams(r.URL.Query()))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	renderHTML(w, insightsPage, data)
}

func (s *Server) handleInsights(w http.ResponseWriter, r *http.Request) {
	resp, err := s.view.Insights(r.Context(), s.clock.Now(), r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, resp)
}
