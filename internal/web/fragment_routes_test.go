package web_test

import (
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
	"github.com/O-Marsters-1997/command-center/internal/web"
)

func fragmentServer(t *testing.T) *web.Server {
	t.Helper()
	st := track(t, openStore(t), alphaRepo, betaRepo)
	storetest.Tickets(t, st, storepkg.Ticket{
		URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first", Feature: "project:x",
	})
	return openServer(st, fixedClock(testNow), "")
}

func TestFragmentRoutesSendAPlainRequestToTheirHostPage(t *testing.T) {
	t.Parallel()

	server := fragmentServer(t)
	for _, tt := range []struct{ name, route, host string }{
		{name: "rail", route: "/rail", host: "/"},
		{name: "board", route: "/board", host: "/tickets"},
		{name: "search", route: "/repos/search?q=api", host: "/repos?q=api"},
		{name: "banner", route: "/repos/banner?repo=acme/alpha", host: "/repos/acme/alpha"},
		{name: "feature candidates", route: "/launch/candidates?feature=project%3Ax", host: "/f/project:x/launch"},
		{name: "ticket candidates", route: "/launch/candidates?ticket=sandbox%3A%2F%2FCC-1",
			host: "/launch?ticket=sandbox%3A%2F%2FCC-1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := do(t, server, http.MethodGet, tt.route, req{})
			if rec.Code != http.StatusSeeOther {
				t.Fatalf("GET %s = %d, want 303", tt.route, rec.Code)
			}
			if got := rec.Location; got != tt.host {
				t.Errorf("Location = %q, want %q", got, tt.host)
			}
			if got := rec.Header.Get("Vary"); got != "HX-Request" {
				t.Errorf("Vary = %q, want HX-Request", got)
			}

			fragment := do(t, server, http.MethodGet, tt.route, req{Header: http.Header{"Hx-Request": {"true"}}})
			if fragment.Code != http.StatusOK {
				t.Fatalf("htmx GET %s = %d, want 200: %s", tt.route, fragment.Code, fragment.Body)
			}
			if got := fragment.Header.Get("Vary"); got != "HX-Request" {
				t.Errorf("htmx Vary = %q, want HX-Request", got)
			}
			if strings.Contains(fragment.Body, "<html") {
				t.Errorf("htmx GET %s answered a full page:\n%s", tt.route, fragment.Body)
			}
		})
	}
}

func TestSessionActionsIsAFragmentOfTheSessionPage(t *testing.T) {
	t.Parallel()

	st, _ := sessionStore(t, sessionRunOpts{Kind: "agent", Prompt: "prompt", KeepPrompt: true})
	server := newServer(st, testNow)

	rec := do(t, server, http.MethodGet, "/s/acme/web/1/actions", req{})
	if rec.Code != http.StatusSeeOther || rec.Location != "/s/acme/web/1" {
		t.Errorf("plain actions = %d %q, want 303 /s/acme/web/1", rec.Code, rec.Location)
	}
	if got := rec.Header.Get("Vary"); got != "HX-Request" {
		t.Errorf("Vary = %q, want HX-Request", got)
	}
	fragment := do(t, server, http.MethodGet, "/s/acme/web/1/actions", req{Header: http.Header{"Hx-Request": {"true"}}})
	if body := fragment.Body; !strings.Contains(body, `id="session-actions"`) {
		t.Errorf("htmx actions is not the actions fragment:\n%s", body)
	}
}

func TestHistoryRestoreOfAFragmentRouteGetsThePage(t *testing.T) {
	t.Parallel()

	response := do(t, fragmentServer(t), http.MethodGet, "/rail", req{
		Header: http.Header{"Hx-Request": {"true"}, "Hx-History-Restore-Request": {"true"}},
	})
	if response.Code != http.StatusSeeOther {
		t.Errorf("history-restore GET /rail = %d, want 303 to the host page", response.Code)
	}
}

func TestCandidatesJSONIsNotAFragment(t *testing.T) {
	t.Parallel()

	rec := do(t, fragmentServer(t), http.MethodGet, "/launch/candidates.json?feature=project%3Ax", req{})
	if rec.Code != http.StatusOK || rec.Header.Get("Content-Type") != "application/json" {
		t.Errorf("plain candidates.json = %d %q, want 200 application/json", rec.Code, rec.Header.Get("Content-Type"))
	}
}

func TestPlainLaunchOpenSendsATicketSliceToThePickerWithTheDialogOpen(t *testing.T) {
	t.Parallel()

	server := fragmentServer(t)
	response := do(t, server, http.MethodPost, "/launch/open", req{Form: url.Values{"ticket": {"sandbox://CC-1"}}})
	if response.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", response.Code)
	}
	location := response.Location
	if want := "/launch?ticket=sandbox%3A%2F%2FCC-1"; location != want {
		t.Fatalf("Location = %q, want %q", location, want)
	}

	page := renderPath(t, server, location)
	if !strings.Contains(page, `<dialog id="launch-modal-body"`) || !strings.Contains(page, `<cc-launch-modal tickets=`) {
		t.Errorf("%s did not render the picker with the dialog open:\n%s", location, page)
	}
}

func TestPlainVerbAndImportPostsGoBackToALocalReturn(t *testing.T) {
	t.Parallel()

	server := newServer(seededRunning(t), testNow)

	for _, tt := range []struct {
		name, target, want string
	}{
		{name: "verb local", target: "/verb?verb=kill&ticket=sandbox://CC-1&return=/f/x", want: "/f/x"},
		{name: "verb absent", target: "/verb?verb=kill&ticket=sandbox://CC-1", want: "/"},
		{name: "verb offsite", target: "/verb?verb=kill&ticket=sandbox://CC-1&return=//evil.example/", want: "/"},
		{name: "verb dotted", target: "/verb?verb=kill&ticket=sandbox://CC-1&return=/a/../b", want: "/"},
		{name: "import local", target: "/features/x/import?return=/f/x", want: "/f/x"},
		{name: "import absent", target: "/features/x/import", want: "/"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			response := do(t, server, http.MethodPost, tt.target, req{})
			if response.Code != http.StatusSeeOther {
				t.Fatalf("status = %d, want 303", response.Code)
			}
			if got := response.Location; got != tt.want {
				t.Errorf("Location = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReposPageRendersTheSearchResultsInThePage(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	page := renderPath(t, reposServer(t, &calls), "/repos?q=api")

	if !strings.Contains(page, `value="api"`) || !strings.Contains(page, "acme/api") {
		t.Errorf("/repos?q=api did not render the search box value and results:\n%s", page)
	}
}
