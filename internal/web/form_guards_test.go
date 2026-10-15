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

	failed, _ := sessionStoreEnding(t, sessionRunOpts{
		Kind: "agent", Prompt: "p", KeepPrompt: true, Outcome: plan.OutcomeFailed,
	})
	pages := []struct {
		name string
		html string
	}{
		{name: "board", html: renderPath(t, newServer(railStore(t, true), testNow), "/tickets")},
		{name: "rail", html: renderPath(t, newServer(railStore(t, true), testNow), "/rail")},
		{name: "session", html: renderPath(t, newServer(failed, testNow), "/s/acme/web/1")},
		{name: "repos", html: renderPath(t, reposServer(t, new(atomic.Int32)), "/repos")},
		{name: "repo", html: renderPath(t, reposServer(t, new(atomic.Int32)), "/repos/acme/new")},
	}

	var seen atomic.Int32
	t.Cleanup(func() {
		if seen.Load() == 0 {
			t.Error("no hx-post form rendered on any page, so the check proves nothing")
		}
	})
	for _, page := range pages {
		t.Run(page.name, func(t *testing.T) {
			t.Parallel()
			for _, form := range formTagRE.FindAllString(page.html, -1) {
				if !strings.Contains(form, "hx-post=") {
					continue
				}
				seen.Add(1)
				if !strings.Contains(form, `hx-disabled-elt="`) {
					t.Errorf("%s page: %s lacks hx-disabled-elt", page.name, form)
				}
			}
		})
	}
}
