package web_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestLegacyIndexURLsRedirectToTheirPathRoutes(t *testing.T) {
	t.Parallel()

	server := threeFeatureServer(t)
	for _, tc := range []struct{ from, want string }{
		{"/?sel=https%3A%2F%2Fgithub.com%2Facme%2Fweb%2Fissues%2F7", "/s/acme/web/7"},
		{"/?feature=project%3Ax", "/f/project:x"},
		{"/?feature=project%3Ax&view=graph", "/f/project:x/graph"},
		{"/?repo=acme/web", "/repos/acme/web"},
		{"/features", "/repos"},
		{"/features?repo=acme/web", "/repos/acme/web"},
		{"/features/project:x", "/f/project:x"},
	} {
		t.Run(tc.from, func(t *testing.T) {
			t.Parallel()

			rec := get(t, server, tc.from)
			if rec.Code != http.StatusMovedPermanently {
				t.Fatalf("GET %s = %d, want 301", tc.from, rec.Code)
			}
			if got := rec.Header().Get("Location"); got != tc.want {
				t.Errorf("GET %s Location = %q, want %q", tc.from, got, tc.want)
			}
		})
	}
}

func TestLegacyIndexURLsWithoutAUsableTargetRenderHome(t *testing.T) {
	t.Parallel()

	server := threeFeatureServer(t)
	for _, from := range []string{"/", "/?repo=../x", "/?sel=sandbox%3A%2F%2FCC-1", "/?view=board"} {
		t.Run(from, func(t *testing.T) {
			t.Parallel()

			rec := get(t, server, from)
			if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `<main id="main" data-home>`) {
				t.Errorf("GET %s = %d, want the home page:\n%s", from, rec.Code, rec.Body)
			}
		})
	}
}

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
