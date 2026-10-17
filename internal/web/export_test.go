package web

import (
	"strings"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

func RenderStatesBoard(states []plan.State) (string, error) {
	board := view.Board{
		Chrome:           view.Chrome{Observe: view.Age{Age: "0s ago"}},
		BoardPollSeconds: config.DefaultBoardPollSeconds,
	}
	var rows []view.Row
	for _, state := range states {
		rows = append(rows, view.Row{
			URL:   "sandbox://" + state.String(),
			State: state.String(),
			Glyph: state.Glyph(),
		})
	}
	board.Sections = view.Sectioned(rows)

	var out strings.Builder
	if err := templates.ExecuteTemplate(&out, "board", board); err != nil {
		return "", err
	}
	return out.String(), nil
}
