package web_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web"
)

func postProjectForm(t *testing.T, server *web.Server, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/projects", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	return rec
}

var soloRepo = storepkg.Repo{Name: "acme/solo", Remote: "git@github.com:acme/solo.git"}

func projectsServer(t *testing.T) *web.Server {
	t.Helper()
	return openServer(track(t, openStore(t), alphaRepo, betaRepo, soloRepo), fixedClock(testNow), "")
}

func TestCreateProjectGroupsItsReposUnderASquare(t *testing.T) {
	t.Parallel()
	server := projectsServer(t)

	rec := postProjectForm(t, server, url.Values{"name": {"acme"}, "hue": {"1"}, "repo": {"acme/alpha", "acme/beta"}})

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /projects = %d, want 303: %s", rec.Code, rec.Body)
	}
	body := renderPath(t, server, "/repos")
	for _, want := range []string{`class="avatar" data-hue="1" aria-hidden="true">A</span>`, "2 repos", "acme/alpha", "acme/beta"} {
		if !strings.Contains(body, want) {
			t.Errorf("GET /repos missing %q:\n%s", want, body)
		}
	}
	if strings.Count(body, `data-scope="project"`) != 1 || strings.Count(body, `data-scope="repo"`) != 1 {
		t.Errorf("GET /repos want one project group and one loose repo:\n%s", body)
	}
}

func TestCreateProjectAnswersHTMXWithTheGroupsFragment(t *testing.T) {
	t.Parallel()
	server := projectsServer(t)
	form := url.Values{"name": {"acme"}, "hue": {"2"}, "repo": {"acme/alpha"}}
	req := httptest.NewRequest(http.MethodPost, "/projects", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `data-hue="2"`) ||
		strings.Contains(rec.Body.String(), "<html") {
		t.Errorf("htmx POST /projects = %d, want the groups fragment:\n%s", rec.Code, rec.Body)
	}
}

func TestCreateProjectRefusesBadInput(t *testing.T) {
	t.Parallel()
	server := projectsServer(t)
	if rec := postProjectForm(t, server, url.Values{"name": {"acme"}, "hue": {"1"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("first create = %d, want 303", rec.Code)
	}

	for name, c := range map[string]struct {
		form url.Values
		want int
	}{
		"malformed name":   {url.Values{"name": {"-bad name"}, "hue": {"1"}}, http.StatusBadRequest},
		"hue out of range": {url.Values{"name": {"other"}, "hue": {"4"}}, http.StatusBadRequest},
		"missing hue":      {url.Values{"name": {"other"}}, http.StatusBadRequest},
		"malformed repo":   {url.Values{"name": {"other"}, "hue": {"1"}, "repo": {"nope"}}, http.StatusBadRequest},
		"case-only dupe":   {url.Values{"name": {"ACME"}, "hue": {"1"}}, http.StatusConflict},
		"untracked repo":   {url.Values{"name": {"other"}, "hue": {"1"}, "repo": {"acme/ghost"}}, http.StatusNotFound},
	} {
		if rec := postProjectForm(t, server, c.form); rec.Code != c.want {
			t.Errorf("%s: POST /projects = %d, want %d: %s", name, rec.Code, c.want, rec.Body)
		}
	}
}

func TestLooseRepoRendersUnderItsOwnNameWithNoSquare(t *testing.T) {
	t.Parallel()
	server := projectsServer(t)

	body := renderPath(t, server, "/repos")

	if !strings.Contains(body, `href="/repos/acme/solo"`) {
		t.Errorf("GET /repos missing the loose repo:\n%s", body)
	}
	if strings.Contains(body, `data-scope="project"`) || strings.Contains(body, `class="avatar" data-hue="1" aria-hidden`) {
		t.Errorf("GET /repos gave a loose repo a project square:\n%s", body)
	}
}

func TestNewProjectFormPreselectsTheLeastUsedHue(t *testing.T) {
	t.Parallel()
	server := projectsServer(t)
	for _, form := range []url.Values{
		{"name": {"one"}, "hue": {"1"}},
		{"name": {"two"}, "hue": {"2"}},
	} {
		if rec := postProjectForm(t, server, form); rec.Code != http.StatusSeeOther {
			t.Fatalf("create %v = %d", form, rec.Code)
		}
	}

	body := renderPath(t, server, "/repos")

	if !strings.Contains(body, `value="3" checked`) || strings.Contains(body, `value="1" checked`) {
		t.Errorf("GET /repos should preselect hue 3:\n%s", body)
	}
}
