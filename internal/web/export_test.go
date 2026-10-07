package web

import (
	"html/template"
	"net/http"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

func RenderStatesPage(states []plan.State) (string, error) {
	return renderStates(page, states)
}

func RenderStatesBoard(states []plan.State) (string, error) {
	return renderStates(boardFragment, states)
}

func renderStates(tmpl *template.Template, states []plan.State) (string, error) {
	board := view.Board{
		Chrome:           view.Chrome{Observe: view.Age{Age: "0s ago"}},
		BoardPollSeconds: config.DefaultBoardPollSeconds,
	}
	for _, state := range states {
		r := view.Row{
			URL:        "sandbox://" + state.String(),
			State:      state.String(),
			Tone:       plan.Tone(state),
			Unattended: state.Unattended(),
			Verbs:      plan.Verbs(state),
		}
		board.Groups = append(board.Groups, view.Group{Children: []view.Row{r}})
	}

	var out strings.Builder
	if err := tmpl.Execute(&out, board); err != nil {
		return "", err
	}
	return out.String(), nil
}

func RenderLogLine(e agentlog.Event, anchor bool) (template.HTML, error) {
	html, err := renderLogLine(e, anchor)
	return template.HTML(html), err
}

func ReadTestdata(name string) string              { return mustReadTestdata(name) }
func WriteRunLog(t *testing.T, body string) string { return writeRunLog(t, body) }

func (s *Server) RegisterTestRoute(pattern string, h http.HandlerFunc) {
	s.rawMux.HandleFunc(pattern, h)
}

func MainTipKey(repo string) string { return plan.BranchKey(repo, "main") }
