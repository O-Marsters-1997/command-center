package web_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web"
)

func fragmentServer(t *testing.T) *web.Server {
	t.Helper()
	st := track(t, openStore(t), alphaRepo, betaRepo)
	if err := st.UpsertTickets(t.Context(), []storepkg.Ticket{
		{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first", Feature: "project:x"},
	}); err != nil {
		t.Fatal(err)
	}
	return openServer(st, fixedClock(testNow), "")
}

func TestFragmentRoutesSendAPlainRequestToTheirHostPage(t *testing.T) {
	t.Parallel()

	server := fragmentServer(t)
	for _, tt := range []struct{ route, host string }{
		{"/rail", "/"},
		{"/board", "/tickets"},
		{"/repos/search?q=api", "/repos?q=api"},
		{"/repos/banner?repo=acme/alpha", "/repos/acme/alpha"},
		{"/launch/candidates?feature=project%3Ax", "/f/project:x/launch"},
		{"/launch/candidates?ticket=sandbox%3A%2F%2FCC-1", "/launch?ticket=sandbox%3A%2F%2FCC-1"},
	} {
		t.Run(tt.route, func(t *testing.T) {
			t.Parallel()

			rec := get(t, server, tt.route)
			if rec.Code != http.StatusSeeOther {
				t.Fatalf("GET %s = %d, want 303", tt.route, rec.Code)
			}
			if got := rec.Header().Get("Location"); got != tt.host {
				t.Errorf("Location = %q, want %q", got, tt.host)
			}
			if got := rec.Header().Get("Vary"); got != "HX-Request" {
				t.Errorf("Vary = %q, want HX-Request", got)
			}

			fragment := getHX(t, server, tt.route)
			if fragment.Code != http.StatusOK {
				t.Fatalf("htmx GET %s = %d, want 200: %s", tt.route, fragment.Code, fragment.Body)
			}
			if got := fragment.Header().Get("Vary"); got != "HX-Request" {
				t.Errorf("htmx Vary = %q, want HX-Request", got)
			}
			if strings.Contains(fragment.Body.String(), "<html") {
				t.Errorf("htmx GET %s answered a full page:\n%s", tt.route, fragment.Body)
			}
		})
	}
}

func TestSessionActionsIsAFragmentOfTheSessionPage(t *testing.T) {
	t.Parallel()

	st, _ := sessionStore(t, "agent", "prompt", true, false)
	server := newServer(st, testNow)

	rec := get(t, server, "/s/acme/web/1/actions")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/s/acme/web/1" {
		t.Errorf("plain actions = %d %q, want 303 /s/acme/web/1", rec.Code, rec.Header().Get("Location"))
	}
	if got := rec.Header().Get("Vary"); got != "HX-Request" {
		t.Errorf("Vary = %q, want HX-Request", got)
	}
	if body := getHX(t, server, "/s/acme/web/1/actions").Body.String(); !strings.Contains(body, `id="session-actions"`) {
		t.Errorf("htmx actions is not the actions fragment:\n%s", body)
	}
}

func TestHistoryRestoreOfAFragmentRouteGetsThePage(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/rail", nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-History-Restore-Request", "true")
	rec := httptest.NewRecorder()
	fragmentServer(t).ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Errorf("history-restore GET /rail = %d, want 303 to the host page", rec.Code)
	}
}

func TestCandidatesJSONIsNotAFragment(t *testing.T) {
	t.Parallel()

	rec := get(t, fragmentServer(t), "/launch/candidates.json?feature=project%3Ax")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("plain candidates.json = %d %q, want 200 application/json", rec.Code, rec.Header().Get("Content-Type"))
	}
}

func TestPlainLaunchOpenSendsATicketSliceToThePickerWithTheDialogOpen(t *testing.T) {
	t.Parallel()

	server := fragmentServer(t)
	srv := httptest.NewServer(server)
	t.Cleanup(srv.Close)

	form := url.Values{"ticket": {"sandbox://CC-1"}}.Encode()
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/launch/open", strings.NewReader(form))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", srv.URL)
	resp, err := noRedirect(srv).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", resp.StatusCode)
	}
	location := resp.Header.Get("Location")
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

	srv := httptest.NewServer(openServer(seededRunning(t), realClock{}, ""))
	t.Cleanup(srv.Close)

	for _, tt := range []struct {
		name, target, want string
	}{
		{"verb local", "/verb?verb=kill&ticket=sandbox://CC-1&return=/f/x", "/f/x"},
		{"verb absent", "/verb?verb=kill&ticket=sandbox://CC-1", "/"},
		{"verb offsite", "/verb?verb=kill&ticket=sandbox://CC-1&return=//evil.example/", "/"},
		{"verb dotted", "/verb?verb=kill&ticket=sandbox://CC-1&return=/a/../b", "/"},
		{"import local", "/features/x/import?return=/f/x", "/f/x"},
		{"import absent", "/features/x/import", "/"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			resp := postVerb(t, srv, tt.target, nil)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusSeeOther {
				t.Fatalf("status = %d, want 303", resp.StatusCode)
			}
			if got := resp.Header.Get("Location"); got != tt.want {
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
