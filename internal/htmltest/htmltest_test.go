package htmltest_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/htmltest"
)

type failingTB struct {
	testing.TB
	message string
}

func (f *failingTB) Helper() {}
func (f *failingTB) Fatalf(format string, args ...any) {
	f.message = fmt.Sprintf(format, args...)
	panic("fatal")
}

func TestSelectors(t *testing.T) {
	t.Parallel()

	page := htmltest.Parse(t, `<div id="board"><section class="group active">`+
		`<a href="/one" data-key="a b">One</a></section> `+
		`<section class="group"><a href="/two">Two</a></section></div>`)
	for _, tc := range []struct {
		name, selector, want string
	}{
		{"tag", "section", "One"},
		{"id", "#board", "One Two"},
		{"class", ".active", "One"},
		{"attribute", "[data-key]", "One"},
		{"attribute value", `[data-key="a b"]`, "One"},
		{"compound descendant", `div#board section.group.active a[href="/one"]`, "One"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			nodes := page.Find(tc.selector)
			if len(nodes) == 0 || nodes[0].Text() != tc.want {
				t.Errorf("Find(%q) = %v, want first text %q", tc.selector, nodes, tc.want)
			}
		})
	}
}

func TestNestedMatchingAncestorsFindOneDescendant(t *testing.T) {
	t.Parallel()

	page := htmltest.Parse(t, `<div><div><a href="/one">One</a></div></div>`)
	if got := page.One(t, "div a").Text(); got != "One" {
		t.Errorf("One(div a) = %q, want One", got)
	}
}

func TestOneReportsItsContainerOnZeroOrMany(t *testing.T) {
	t.Parallel()

	page := htmltest.Parse(t, `<div id="board"><b>one</b><b>two</b></div>`)
	for _, selector := range []string{"span", "b"} {
		t.Run(selector, func(t *testing.T) {
			t.Parallel()
			fake := &failingTB{}
			func() {
				defer func() { _ = recover() }()
				page.One(fake, selector)
			}()
			if !strings.Contains(fake.message, page.HTML()) {
				t.Errorf("One(%q) failure = %q, want container HTML", selector, fake.message)
			}
		})
	}
}

func TestTextCollapsesWhitespaceAndAttrDistinguishesMissing(t *testing.T) {
	t.Parallel()

	node := htmltest.Parse(t, "<p title=\"\"> one \n <em>two</em>   three </p>").One(t, "p")
	if got := node.Text(); got != "one two three" {
		t.Errorf("Text() = %q", got)
	}
	if got := htmltest.Parse(t, `<p><span>foo</span>bar</p>`).One(t, "p").Text(); got != "foobar" {
		t.Errorf("Text() without source whitespace = %q, want foobar", got)
	}
	if value, ok := node.Attr("title"); !ok || value != "" {
		t.Errorf("Attr(title) = %q, %t", value, ok)
	}
	if _, ok := node.Attr("missing"); ok {
		t.Error("Attr(missing) should be absent")
	}
}
