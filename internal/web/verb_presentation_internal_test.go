package web

import (
	"regexp"
	"strings"
	"testing"
)

func TestBoardAndSessionTemplatesDoNotDecideDomainPresentation(t *testing.T) {
	t.Parallel()
	fieldComparison := regexp.MustCompile(`\{\{[^}]*\b(?:eq|ne)\s+[^}]*\.(?:Key|Glyph|Verb)\b[^}]*\}\}`)
	implicitComparison := regexp.MustCompile(`\{\{[^}]*\b(?:eq|ne)\s+\.\s+"(?:` +
		`failed|attention|running|pending|checking|ready|blocked|done|needs-you|in-progress|` +
		`launch|kill|re-run|retry-push|remove-worktree|cancel|refresh|abort|follow-up|import|track` +
		`)"[^}]*\}\}`)
	files, err := templateFiles.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		name := file.Name()
		if !strings.HasSuffix(name, ".tmpl") {
			continue
		}
		body, err := templateFiles.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if match := fieldComparison.Find(body); match != nil {
			t.Errorf("%s compares domain value: %s", name, match)
		}
		if (name == "board.tmpl" || name == "session.tmpl") && implicitComparison.Match(body) {
			t.Errorf("%s compares an implicit verb, glyph or section value", name)
		}
		if name == "board.tmpl" && (strings.Contains(string(body), "sectionGlyph") ||
			strings.Contains(string(body), "sectionStat")) {
			t.Errorf("%s still maps sections in the template", name)
		}
	}
}
