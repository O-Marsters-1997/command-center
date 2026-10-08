package web_test

import (
	"os"
	"strings"
	"testing"
)

func TestBuiltStylesheetScansTemplates(t *testing.T) {
	css, err := os.ReadFile("assets/dist/app.css")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(css), ".sr-only{") {
		t.Fatal("assets/dist/app.css has no .sr-only rule: web/app.css's @source declaration " +
			"is no longer reaching internal/web/testdata/sourcecheck.tmpl (rerun `just assets` after fixing)")
	}
}
