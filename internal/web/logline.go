package web

import (
	"fmt"
	"strings"

	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

func renderLogLine(line view.LogLine) (string, error) {
	line.Verb, line.Arg = flatten(line.Verb), flatten(line.Arg)
	var buf strings.Builder
	if err := templates.ExecuteTemplate(&buf, "logline", line); err != nil {
		return "", fmt.Errorf("render log line: %w", err)
	}
	return buf.String(), nil
}

func flatten(s string) string {
	return strings.ReplaceAll(s, "\n", " ")
}
