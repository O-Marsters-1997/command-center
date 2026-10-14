package web_test

import (
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

var formTagRE = regexp.MustCompile(`<form\b[^>]*>`)

func TestEveryHTMXPostFormDisablesItsButtonsInFlight(t *testing.T) {
	t.Parallel()

	failed, _ := sessionStoreEnding(t, "agent", "p", true, false, plan.OutcomeFailed)
	pages := []struct {
		name string
		html string
	}{
		{"board", renderPath(t, newServer(railStore(t, true), testNow), "/tickets")},
		{"rail", renderPath(t, newServer(railStore(t, true), testNow), "/rail")},
		{"session", renderPath(t, newServer(failed, testNow), "/s/acme/web/1")},
		{"repos", renderPath(t, reposServer(t, new(atomic.Int32)), "/repos")},
		{"repo", renderPath(t, reposServer(t, new(atomic.Int32)), "/repos/acme/new")},
	}

	seen := 0
	for _, page := range pages {
		for _, form := range formTagRE.FindAllString(page.html, -1) {
			if !strings.Contains(form, "hx-post=") {
				continue
			}
			seen++
			if !strings.Contains(form, `hx-disabled-elt="find button"`) {
				t.Errorf("%s page: %s lacks hx-disabled-elt", page.name, form)
			}
		}
	}
	if seen == 0 {
		t.Fatal("no hx-post form rendered on any page, so the check proves nothing")
	}
}
