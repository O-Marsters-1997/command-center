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
	square := `class="avatar" data-hue="1" aria-hidden="true">A</span>`
	for _, want := range []string{square, "2 repos", "acme/alpha", "acme/beta"} {
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
	projectGroup := `data-scope="project"`
	square := `class="avatar" data-hue="1" aria-hidden`
	if strings.Contains(body, projectGroup) || strings.Contains(body, square) {
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

	page := renderPath(t, server, "/repos")
	_, body, _ := strings.Cut(page, "<summary>New project</summary>")
	body, _, _ = strings.Cut(body, "</details>")

	if !strings.Contains(body, `value="3" checked`) || strings.Contains(body, `value="1" checked`) {
		t.Errorf("GET /repos should preselect hue 3:\n%s", body)
	}
}

func postPath(t *testing.T, server *web.Server, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	return rec
}

func TestEditProjectRenamesAndKeepsReposAndHue(t *testing.T) {
	t.Parallel()
	server := projectsServer(t)
	postProjectForm(t, server, url.Values{"name": {"acme"}, "hue": {"2"}, "repo": {"acme/alpha"}})

	rec := postPath(t, server, "/projects/acme", url.Values{"name": {"acme-co"}, "hue": {"2"}})

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /projects/acme = %d, want 303: %s", rec.Code, rec.Body)
	}
	body := renderPath(t, server, "/repos")
	if !strings.Contains(body, `data-hue="2" aria-hidden="true">A</span>`) || !strings.Contains(body, "acme-co") ||
		!strings.Contains(body, "1 repo<") {
		t.Errorf("GET /repos after rename:\n%s", body)
	}
}

func TestEditProjectRefusals(t *testing.T) {
	t.Parallel()
	server := projectsServer(t)
	postProjectForm(t, server, url.Values{"name": {"acme"}, "hue": {"1"}})
	postProjectForm(t, server, url.Values{"name": {"beta"}, "hue": {"1"}})

	for name, c := range map[string]struct {
		path string
		form url.Values
		want int
	}{
		"onto existing": {"/projects/acme", url.Values{"name": {"beta"}, "hue": {"1"}}, http.StatusConflict},
		"unknown":       {"/projects/ghost", url.Values{"name": {"x"}, "hue": {"1"}}, http.StatusNotFound},
		"bad hue":       {"/projects/acme", url.Values{"name": {"acme"}, "hue": {"9"}}, http.StatusBadRequest},
		"delete ghost":  {"/projects/ghost/delete", nil, http.StatusNotFound},
		"move unknown project": {
			"/repos/acme/alpha/project", url.Values{"project": {"ghost"}}, http.StatusNotFound,
		},
		"move unknown repo": {"/repos/acme/ghost/project", url.Values{"project": {"acme"}}, http.StatusNotFound},
	} {
		if rec := postPath(t, server, c.path, c.form); rec.Code != c.want {
			t.Errorf("%s: POST %s = %d, want %d: %s", name, c.path, rec.Code, c.want, rec.Body)
		}
	}
}

func TestDeleteProjectLeavesReposLoose(t *testing.T) {
	t.Parallel()
	server := projectsServer(t)
	postProjectForm(t, server, url.Values{"name": {"acme"}, "hue": {"1"}, "repo": {"acme/alpha"}})

	if rec := postPath(t, server, "/projects/acme/delete", nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("delete = %d, want 303: %s", rec.Code, rec.Body)
	}

	body := renderPath(t, server, "/repos")
	if strings.Contains(body, `data-scope="project"`) || !strings.Contains(body, `href="/repos/acme/alpha"`) {
		t.Errorf("GET /repos after delete:\n%s", body)
	}
}

func TestRepoSelectMovesAndLoosensARepo(t *testing.T) {
	t.Parallel()
	server := projectsServer(t)
	postProjectForm(t, server, url.Values{"name": {"acme"}, "hue": {"1"}})

	if rec := postPath(t, server, "/repos/acme/solo/project", url.Values{"project": {"acme"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("move = %d: %s", rec.Code, rec.Body)
	}
	if body := renderPath(t, server, "/repos"); !strings.Contains(body, `data-scope="project"`) ||
		!strings.Contains(body, `<option value="acme" selected>`) {
		t.Errorf("GET /repos after move:\n%s", body)
	}

	if rec := postPath(t, server, "/repos/acme/solo/project", url.Values{"project": {""}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("loosen = %d: %s", rec.Code, rec.Body)
	}
	if body := renderPath(t, server, "/repos"); strings.Contains(body, `<option value="acme" selected>`) {
		t.Errorf("GET /repos after loosen still selects acme:\n%s", body)
	}
}
