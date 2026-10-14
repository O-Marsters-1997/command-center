package web_test

import (
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestHomeCarriesTheTicketsPageAsItsDesktopTarget(t *testing.T) {
	t.Parallel()

	body := renderPath(t, newServer(seededStore(t, testNow), testNow), "/")

	if !strings.Contains(body, `data-desktop-target="/tickets"`) {
		t.Errorf("home does not name /tickets as its desktop target:\n%s", body)
	}
}

func TestHomeCarriesASessionThatNeedsTheUserAsItsDesktopTarget(t *testing.T) {
	t.Parallel()

	st, _ := sessionStoreEnding(t, sessionRunOpts{Kind: "agent", Outcome: plan.OutcomeFailed})

	body := renderPath(t, newServer(st, testNow), "/")

	if !strings.Contains(body, `data-desktop-target="/s/acme/web/1"`) {
		t.Errorf("home does not name the failed session as its desktop target:\n%s", body)
	}
}
