package web

import (
	_ "embed"
	"html/template"
	"strings"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
)

//go:embed logline.tmpl
var loglineSource string

// Registered into page's own template set so detail.tmpl can call {{template "logline" ...}}
// and sendLines can render a streamed line through the identical definition
// (docs/prds/prd-fleet-view.md § One template renders a log line).
var _ = template.Must(page.New("logline").Parse(loglineSource))

type logLineData struct {
	Kind   string
	Tool   string
	Detail string
	Anchor bool
}

// renderLogLine is the one place an agentlog.Event becomes markup: the detail render and the SSE
// stream both call it, so the same event line reads identically wherever it arrived from.
func renderLogLine(e agentlog.Event, anchor bool) (string, error) {
	data := logLineData{Kind: e.Kind.String(), Tool: flatten(e.Tool), Detail: flatten(e.Detail), Anchor: anchor}
	var buf strings.Builder
	if err := page.ExecuteTemplate(&buf, "logline", data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// flatten keeps a rendered line on one physical line: the SSE wire format is one "data:" line
// per event, and a tool's own input can carry an embedded newline (a multi-line Bash command,
// for one).
func flatten(s string) string {
	return strings.ReplaceAll(s, "\n", " ")
}
