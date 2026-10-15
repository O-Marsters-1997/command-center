package web_test

import (
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"
)

func requireBuiltStylesheet(t *testing.T) {
	t.Helper()
	if _, err := os.Stat("assets/dist/app.css"); errors.Is(err, fs.ErrNotExist) {
		t.Skip("assets/dist/app.css is not built; run `just assets`")
	}
}

func TestBuiltStylesheetScansTemplates(t *testing.T) {
	t.Parallel()

	requireBuiltStylesheet(t)
	css, err := os.ReadFile("assets/dist/app.css")
	if err != nil {
		t.Fatalf("ReadFile(assets/dist/app.css): %v", err)
	}
	if !strings.Contains(string(css), ".sr-only{") {
		t.Fatal("assets/dist/app.css has no .sr-only rule: web/app.css's @source declaration " +
			"is no longer reaching internal/web/testdata/sourcecheck.tmpl (rerun `just assets` after fixing)")
	}
}
