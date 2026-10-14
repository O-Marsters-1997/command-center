package web_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	scriptTag     = regexp.MustCompile(`(?i)<script\b[^>]*>`)
	scriptSrc     = regexp.MustCompile(`(?i)\bsrc=`)
	inlineHandler = regexp.MustCompile(`(?i)<[a-z][^>]*\son[a-z]+\s*=`)
	hxOn          = regexp.MustCompile(`(?i)\bhx-on`)
)

func TestTemplatesCarryNoJavaScript(t *testing.T) {
	t.Parallel()

	files, err := filepath.Glob("*.tmpl")
	if err != nil || len(files) == 0 {
		t.Fatalf("Glob(*.tmpl) = %v, %v", files, err)
	}
	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		text := string(src)
		for _, tag := range scriptTag.FindAllString(text, -1) {
			if !scriptSrc.MatchString(tag) {
				t.Errorf("%s has an inline script: %s", file, tag)
			}
		}
		if m := inlineHandler.FindString(text); m != "" {
			t.Errorf("%s has an inline event handler: %q", file, m)
		}
		if hxOn.MatchString(text) {
			t.Errorf("%s has an hx-on attribute", file)
		}
	}
}

func TestHTMXConfigForbidsEvalAndInjectedStyles(t *testing.T) {
	t.Parallel()

	body := renderPath(t, newServer(seededStore(t, testNow), testNow), "/tickets")

	for _, want := range []string{`"allowEval":false`, `"includeIndicatorStyles":false`} {
		if !strings.Contains(body, want) {
			t.Errorf("htmx-config is missing %s:\n%s", want, body)
		}
	}
}
