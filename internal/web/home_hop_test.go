package web_test

import (
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestHomeHopsToTheTicketsPageWhenNothingNeedsTheUser(t *testing.T) {
	t.Parallel()

	body := unescapeJSSlashes(renderPath(t, newServer(seededStore(t, testNow), testNow), "/"))

	if !strings.Contains(body, `location.replace("/tickets")`) {
		t.Errorf("home does not hop a desktop to /tickets:\n%s", body)
	}
}

func TestHomeHopsToASessionThatNeedsTheUser(t *testing.T) {
	t.Parallel()

	st, _ := sessionStoreEnding(t, "agent", "", false, false, plan.OutcomeFailed)

	body := unescapeJSSlashes(renderPath(t, newServer(st, testNow), "/"))

	if !strings.Contains(body, `location.replace("/s/acme/web/1")`) {
		t.Errorf("home does not hop a desktop to the failed session:\n%s", body)
	}
}

func unescapeJSSlashes(s string) string { return strings.ReplaceAll(s, `\/`, "/") }
