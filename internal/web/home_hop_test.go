package web_test

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/htmltest"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestHomeCarriesTheTicketsPageAsItsDesktopTarget(t *testing.T) {
	t.Parallel()

	body := renderPath(t, newServer(seededStore(t, testNow), testNow), "/")

	main := htmltest.Parse(t, body).One(t, "main#main")
	if got, _ := main.Attr("data-desktop-target"); got != "/tickets" {
		t.Errorf("home data-desktop-target = %q, want /tickets", got)
	}
}

func TestHomeCarriesASessionThatNeedsTheUserAsItsDesktopTarget(t *testing.T) {
	t.Parallel()

	st, _ := sessionStoreEnding(t, sessionRunOpts{Kind: "agent", Outcome: plan.OutcomeFailed})

	body := renderPath(t, newServer(st, testNow), "/")

	main := htmltest.Parse(t, body).One(t, "main#main")
	if got, _ := main.Attr("data-desktop-target"); got != "/s/acme/web/1" {
		t.Errorf("home data-desktop-target = %q, want /s/acme/web/1", got)
	}
}
