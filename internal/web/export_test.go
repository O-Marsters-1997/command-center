package web

import (
	"html/template"
	"slices"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

func RenderStatesPage(states []plan.State) (string, error) {
	return renderStates("tickets.tmpl", states)
}

func RenderStatesBoard(states []plan.State) (string, error) {
	return renderStates("board", states)
}

func renderStates(name string, states []plan.State) (string, error) {
	board := view.Board{
		Chrome:           view.Chrome{Observe: view.Age{Age: "0s ago"}},
		BoardPollSeconds: config.DefaultBoardPollSeconds,
	}
	var rows []view.Row
	for _, state := range states {
		r := view.Row{
			URL:        "sandbox://" + state.String(),
			State:      state.String(),
			Glyph:      state.Glyph(),
			Unattended: state.Unattended(),
			Verbs:      plan.Verbs(state),
		}
		r.Launchable = slices.Contains(r.Verbs, plan.VerbLaunch)
		r.Buttons = r.VerbButtons()
		rows = append(rows, r)
	}
	board.Sections = view.Sectioned(rows)

	var out strings.Builder
	if err := templates.ExecuteTemplate(&out, name, board); err != nil {
		return "", err
	}
	return out.String(), nil
}

func RenderLogLine(e agentlog.Event, anchor bool) (template.HTML, error) {
	line := view.LineOf(e)
	line.Anchor = anchor
	return renderLogLine(line)
}

func ReadTestdata(name string) string { return mustReadTestdata(name) }
func WriteRunLog(t *testing.T, body string) string {
	t.Helper()
	return writeRunLog(t, body)
}

func MainTipKey(repo string) plan.BranchRef { return plan.BranchRef{Repo: repo, Branch: "main"} }
