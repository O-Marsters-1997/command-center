package web

import (
	_ "embed"
	"html/template"
	"strings"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
)

//go:embed logline.tmpl
var loglineSource string

var _ = template.Must(page.New("logline").Parse(loglineSource))

type logLineData struct {
	Kind   string
	Tool   string
	Detail string
	Anchor bool
}

func renderLogLine(e agentlog.Event, anchor bool) (string, error) {
	data := logLineData{Kind: e.Kind.String(), Tool: flatten(e.Tool), Detail: flatten(e.Detail), Anchor: anchor}
	var buf strings.Builder
	if err := page.ExecuteTemplate(&buf, "logline", data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// flatten keeps a rendered line on one physical line: SSE carries one "data:" line per event.
func flatten(s string) string {
	return strings.ReplaceAll(s, "\n", " ")
}
