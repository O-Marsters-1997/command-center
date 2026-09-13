package cc_test

import (
	"os"
	"strings"
	"testing"
)

// TestBuiltStylesheetScansTemplates fails if internal/cc stops being scanned into the built
// sheet: Tailwind's CLI runs from web/ and never walks up into internal/cc on its own.
func TestBuiltStylesheetScansTemplates(t *testing.T) {
	css, err := os.ReadFile("assets/dist/app.css")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(css), ".sr-only{") {
		t.Fatal("assets/dist/app.css has no .sr-only rule: web/app.css's @source declaration " +
			"is no longer reaching internal/cc/testdata/sourcecheck.tmpl (rerun `just assets` after fixing)")
	}
}
