package web_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/adapter/tracker"
	"github.com/O-Marsters-1997/command-center/internal/adapter/tracker/trackertest"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web"
	"github.com/O-Marsters-1997/command-center/internal/web/htmltest"
)

var soloRepo = storepkg.Repo{Name: "acme/solo", Remote: "git@github.com:acme/solo.git"}

func projectsServer(t *testing.T) *web.Server {
	t.Helper()
	empty := &trackertest.Source{}
	return openServer(track(t, openStore(t), alphaRepo, betaRepo, soloRepo), fixedClock(testNow), "",
		web.WithTrackerSource(trackertest.ByRemote(map[string]tracker.Source{
			"github.com/acme/alpha": empty, "github.com/acme/beta": empty, "github.com/acme/solo": empty,
		})))
}

func TestProjectsPage(t *testing.T) {
	t.Parallel()

	for name, run := range map[string]func(*testing.T){
		"create groups its repos under a square":   testCreateProjectGroups,
		"create answers htmx with the fragment":    testCreateProjectHTMX,
		"create refuses bad input":                 testCreateProjectRefusesBadInput,
		"a loose repo has no square":               testLooseRepo,
		"new project form preselects the free hue": testNewProjectFormHue,
		"edit renames and keeps repos and hue":     testEditProjectRenames,
		"edit refusals":                            testEditProjectRefusals,
		"delete leaves repos loose":                testDeleteProject,
		"repo select moves and loosens a repo":     testRepoSelectMoves,
	} {
		t.Run(name, run)
	}
}

func testCreateProjectGroups(t *testing.T) {
	t.Helper()
	t.Parallel()
	server := projectsServer(t)

	form := url.Values{"name": {"acme"}, "hue": {"1"}, "repo": {"acme/alpha", "acme/beta"}}
	rec := do(t, server, http.MethodPost, "/projects", req{Form: form})

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /projects = %d, want 303", rec.Code)
	}
	body := renderPath(t, server, "/repos")
	root := htmltest.Parse(t, body).One(t, "main#main")
	group := root.One(t, `[data-scope="project"]`)
	if avatar := group.One(t, `[data-hue="1"][aria-hidden="true"]`); avatar.Text() != "A" {
		t.Errorf("project avatar = %q: %s", avatar.Text(), avatar.HTML())
	}
	for _, href := range []string{"/repos/acme/alpha", "/repos/acme/beta"} {
		group.One(t, `a[href="`+href+`"]`)
	}
	if got := group.Text(); !strings.Contains(got, "2 repos") {
		t.Errorf("project group text = %q", got)
	}
	root.One(t, `[data-scope="repo"]`)
}

func testCreateProjectHTMX(t *testing.T) {
	t.Helper()
	t.Parallel()
	server := projectsServer(t)
	form := url.Values{"name": {"acme"}, "hue": {"2"}, "repo": {"acme/alpha"}, "return": {"/repos/acme/alpha"}}
	rec := do(t, server, http.MethodPost, "/projects", req{Form: form, Header: http.Header{"Hx-Request": {"true"}}})

	if rec.Code != http.StatusNoContent {
		t.Fatalf("htmx POST /projects = %d, want 204", rec.Code)
	}
	if got := rec.Header.Get("HX-Location"); got != "/repos/acme/alpha" {
		t.Errorf("HX-Location = %q, want /repos/acme/alpha", got)
	}
}

func testCreateProjectRefusesBadInput(t *testing.T) {
	t.Helper()
	server := projectsServer(t)
	form := url.Values{"name": {"acme"}, "hue": {"1"}}
	if rec := do(t, server, http.MethodPost, "/projects", req{Form: form}); rec.Code != http.StatusSeeOther {
		t.Fatalf("first create = %d, want 303", rec.Code)
	}

	for _, c := range []struct {
		name string
		form url.Values
		want int
	}{
		{name: "malformed name", form: url.Values{"name": {"-bad name"}, "hue": {"1"}}, want: http.StatusBadRequest},
		{name: "hue out of range", form: url.Values{"name": {"other"}, "hue": {"4"}}, want: http.StatusBadRequest},
		{name: "missing hue", form: url.Values{"name": {"other"}}, want: http.StatusBadRequest},
		{name: "malformed repo", form: url.Values{"name": {"other"}, "hue": {"1"}, "repo": {"nope"}},
			want: http.StatusBadRequest},
		{name: "case-only dupe", form: url.Values{"name": {"ACME"}, "hue": {"1"}}, want: http.StatusConflict},
		{name: "untracked repo", form: url.Values{"name": {"other"}, "hue": {"1"}, "repo": {"acme/ghost"}},
			want: http.StatusNotFound},
	} {
		t.Run(c.name, func(t *testing.T) {
			before := renderPath(t, server, "/repos")
			if rec := do(t, server, http.MethodPost, "/projects", req{Form: c.form}); rec.Code != c.want {
				t.Errorf("POST /projects = %d, want %d", rec.Code, c.want)
			}
			if after := renderPath(t, server, "/repos"); after != before {
				t.Errorf("POST /projects changed the repos page on refusal; project groups = %d, want %d",
					len(htmltest.Parse(t, after).Find(`[data-scope="project"]`)),
					len(htmltest.Parse(t, before).Find(`[data-scope="project"]`)))
			}
		})
	}
}

func testLooseRepo(t *testing.T) {
	t.Helper()
	t.Parallel()
	server := projectsServer(t)

	body := renderPath(t, server, "/repos")

	root := htmltest.Parse(t, body).One(t, "main#main")
	loose := root.One(t, `[data-scope="repo"]`)
	loose.One(t, `a[href="/repos/acme/solo"]`)
	if got := loose.One(t, "h2").Text(); !strings.Contains(got, "No project") {
		t.Errorf("loose group heading = %q", got)
	}
	if projects := root.Find(`[data-scope="project"]`); len(projects) != 0 {
		t.Errorf("loose repo grouped under project: %s", projects[0].HTML())
	}
	if avatars := loose.Find("[data-hue]"); len(avatars) != 0 {
		t.Errorf("loose repo has a project square: %s", avatars[0].HTML())
	}
	if forms := root.Find("form[action^='/projects'], form[action$='/project']"); len(forms) != 0 {
		t.Errorf("repos page manages projects: %s", forms[0].HTML())
	}
}

func testNewProjectFormHue(t *testing.T) {
	t.Helper()
	server := projectsServer(t)
	for _, tt := range []struct {
		name string
		form url.Values
	}{
		{name: "first hue", form: url.Values{"name": {"one"}, "hue": {"1"}}},
		{name: "second hue", form: url.Values{"name": {"two"}, "hue": {"2"}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if rec := do(t, server, http.MethodPost, "/projects", req{Form: tt.form}); rec.Code != http.StatusSeeOther {
				t.Fatalf("create %v = %d", tt.form, rec.Code)
			}
		})
	}

	page := htmltest.Parse(t, renderPath(t, server, "/repos/acme/solo")).One(t, `form[action="/projects"]`)
	page.One(t, `input[name="hue"][value="3"][checked]`)
	if first := page.Find(`input[name="hue"][value="1"][checked]`); len(first) != 0 {
		t.Errorf("hue 1 still preselected: %s", first[0].HTML())
	}
}

func testEditProjectRenames(t *testing.T) {
	t.Helper()
	t.Parallel()
	server := projectsServer(t)
	form := url.Values{"name": {"acme"}, "hue": {"2"}, "repo": {"acme/alpha"}}
	do(t, server, http.MethodPost, "/projects", req{Form: form})

	rec := do(t, server, http.MethodPost, "/projects/acme", req{Form: url.Values{"name": {"acme-co"}, "hue": {"2"}}})

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /projects/acme = %d, want 303", rec.Code)
	}
	body := renderPath(t, server, "/repos")
	group := htmltest.Parse(t, body).One(t, `[data-scope="project"]`)
	if got := group.One(t, `[data-hue="2"][aria-hidden="true"]`).Text(); got != "A" {
		t.Errorf("renamed project avatar = %q", got)
	}
	if got := group.Text(); !strings.Contains(got, "acme-co") || !strings.Contains(got, "1 repo") {
		t.Errorf("renamed project group = %q", got)
	}
}

func testEditProjectRefusals(t *testing.T) {
	t.Helper()
	server := projectsServer(t)
	do(t, server, http.MethodPost, "/projects", req{Form: url.Values{"name": {"acme"}, "hue": {"1"}}})
	do(t, server, http.MethodPost, "/projects", req{Form: url.Values{"name": {"beta"}, "hue": {"1"}}})

	for _, c := range []struct {
		name string
		path string
		form url.Values
		want int
	}{
		{name: "onto existing", path: "/projects/acme", form: url.Values{"name": {"beta"}, "hue": {"1"}},
			want: http.StatusConflict},
		{name: "unknown", path: "/projects/ghost", form: url.Values{"name": {"x"}, "hue": {"1"}}, want: http.StatusNotFound},
		{name: "bad hue", path: "/projects/acme", form: url.Values{"name": {"acme"}, "hue": {"9"}},
			want: http.StatusBadRequest},
		{name: "delete ghost", path: "/projects/ghost/delete", want: http.StatusNotFound},
		{name: "move unknown project", path: "/repos/acme/alpha/project", form: url.Values{"project": {"ghost"}},
			want: http.StatusNotFound},
		{name: "move unknown repo", path: "/repos/acme/ghost/project", form: url.Values{"project": {"acme"}},
			want: http.StatusNotFound},
	} {
		t.Run(c.name, func(t *testing.T) {
			before := renderPath(t, server, "/repos")
			if rec := do(t, server, http.MethodPost, c.path, req{Form: c.form}); rec.Code != c.want {
				t.Errorf("POST %s = %d, want %d", c.path, rec.Code, c.want)
			}
			if after := renderPath(t, server, "/repos"); after != before {
				t.Errorf("POST %s changed the repos page on refusal; project groups = %d, want %d",
					c.path, len(htmltest.Parse(t, after).Find(`[data-scope="project"]`)),
					len(htmltest.Parse(t, before).Find(`[data-scope="project"]`)))
			}
		})
	}
}

func testDeleteProject(t *testing.T) {
	t.Helper()
	t.Parallel()
	server := projectsServer(t)
	form := url.Values{"name": {"acme"}, "hue": {"1"}, "repo": {"acme/alpha"}}
	do(t, server, http.MethodPost, "/projects", req{Form: form})

	if rec := do(t, server, http.MethodPost, "/projects/acme/delete", req{}); rec.Code != http.StatusSeeOther {
		t.Fatalf("delete = %d, want 303", rec.Code)
	}

	body := renderPath(t, server, "/repos")
	root := htmltest.Parse(t, body).One(t, "main#main")
	root.One(t, `a[href="/repos/acme/alpha"]`)
	if groups := root.Find(`[data-scope="project"]`); len(groups) != 0 {
		t.Errorf("deleted project group persists: %s", groups[0].HTML())
	}
}

func testRepoSelectMoves(t *testing.T) {
	t.Helper()
	t.Parallel()
	server := projectsServer(t)
	do(t, server, http.MethodPost, "/projects", req{Form: url.Values{"name": {"acme"}, "hue": {"1"}}})

	move := url.Values{"project": {"acme"}, "return": {"/repos/acme/solo"}}
	moveRec := do(t, server, http.MethodPost, "/repos/acme/solo/project", req{Form: move})
	if moveRec.Code != http.StatusSeeOther {
		t.Fatalf("move = %d, want 303", moveRec.Code)
	}
	if got := moveRec.Header.Get("Location"); got != "/repos/acme/solo" {
		t.Errorf("move Location = %q, want /repos/acme/solo", got)
	}
	group := htmltest.Parse(t, renderPath(t, server, "/repos")).One(t, `main#main [data-scope="project"]`)
	group.One(t, `a[href="/repos/acme/solo"]`)
	root := htmltest.Parse(t, renderPath(t, server, "/repos/acme/solo")).One(t, "main#main")
	root.One(t, `form[action="/repos/acme/solo/project"] option[value="acme"][selected]`)

	loosen := url.Values{"project": {""}}
	loosenRec := do(t, server, http.MethodPost, "/repos/acme/solo/project", req{Form: loosen})
	if loosenRec.Code != http.StatusSeeOther {
		t.Fatalf("loosen = %d, want 303", loosenRec.Code)
	}
	root = htmltest.Parse(t, renderPath(t, server, "/repos/acme/solo")).One(t, "main#main")
	selection := root.One(t, `form[action="/repos/acme/solo/project"]`)
	selection.One(t, `option[value="acme"]`)
	if selected := selection.Find(`option[value="acme"][selected]`); len(selected) != 0 {
		t.Errorf("loosened repo still selects acme: %s", selected[0].HTML())
	}
}
