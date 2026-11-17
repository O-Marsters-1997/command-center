package plan

import (
	"fmt"
	"strings"
)

type Side string

const (
	SideOld Side = "old"
	SideNew Side = "new"
)

type ReviewComment struct {
	ID   int64
	Tip  SHA
	Path string
	Line int
	Side Side
	Hunk string // the hunk header and lines up to and including the commented line
	Body string
}

func ComposeReview(note string, comments []ReviewComment) string {
	var b strings.Builder
	b.WriteString("Changes requested on this branch.")
	if note = strings.TrimSpace(note); note != "" {
		b.WriteString("\n\n" + note)
	}
	for _, c := range comments {
		fmt.Fprintf(&b, "\n\n%s, %s line %d:\n```diff\n%s\n```\n%s", c.Path, c.Side, c.Line, c.Hunk, c.Body)
	}
	return b.String()
}
