package web

import (
	"strings"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
)

type logLineData struct {
	Kind   string
	Tool   string
	Detail string
	Anchor bool
}

func renderLogLine(e agentlog.Event, anchor bool) (string, error) {
	data := logLineData{Kind: e.Kind.String(), Tool: flatten(e.Tool), Detail: flatten(e.Detail), Anchor: anchor}
	var buf strings.Builder
	if err := templates.ExecuteTemplate(&buf, "logline", data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// flatten keeps a rendered line on one physical line: SSE carries one "data:" line per event.
func flatten(s string) string {
	return strings.ReplaceAll(s, "\n", " ")
}
