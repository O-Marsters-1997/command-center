package web_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web"
)

var soloRepo = storepkg.Repo{Name: "acme/solo", Remote: "git@github.com:acme/solo.git"}

func projectsServer(t *testing.T) *web.Server {
	t.Helper()
	return openServer(track(t, openStore(t), alphaRepo, betaRepo, soloRepo), fixedClock(testNow), "")
}

func TestCreateProjectGroupsItsReposUnderASquare(t *testing.T) {
	t.Parallel()
	server := projectsServer(t)

	form := url.Values{"name": {"acme"}, "hue": {"1"}, "repo": {"acme/alpha", "acme/beta"}}
	rec := do(t, server, http.MethodPost, "/projects", req{Form: form})

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
	rec := do(t, server, http.MethodPost, "/projects", req{Form: form, Header: http.Header{"Hx-Request": {"true"}}})

	if rec.Code != http.StatusOK || !strings.Contains(rec.Body, `data-hue="2"`) ||
		strings.Contains(rec.Body, "<html") {
		t.Errorf("htmx POST /projects = %d, want the groups fragment:\n%s", rec.Code, rec.Body)
	}
}

func TestCreateProjectRefusesBadInput(t *testing.T) {
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
				t.Errorf("POST /projects = %d, want %d: %s", rec.Code, c.want, rec.Body)
			}
			if after := renderPath(t, server, "/repos"); after != before {
				t.Errorf("POST /projects changed the repos page on refusal:\n%s", after)
			}
		})
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

	page := renderPath(t, server, "/repos")
	_, body, _ := strings.Cut(page, "<summary>New project</summary>")
	body, _, _ = strings.Cut(body, "</details>")

	if !strings.Contains(body, `value="3" checked`) || strings.Contains(body, `value="1" checked`) {
		t.Errorf("GET /repos should preselect hue 3:\n%s", body)
	}
}

func TestEditProjectRenamesAndKeepsReposAndHue(t *testing.T) {
	t.Parallel()
	server := projectsServer(t)
	form := url.Values{"name": {"acme"}, "hue": {"2"}, "repo": {"acme/alpha"}}
	do(t, server, http.MethodPost, "/projects", req{Form: form})

	rec := do(t, server, http.MethodPost, "/projects/acme", req{Form: url.Values{"name": {"acme-co"}, "hue": {"2"}}})

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
				t.Errorf("POST %s = %d, want %d: %s", c.path, rec.Code, c.want, rec.Body)
			}
			if after := renderPath(t, server, "/repos"); after != before {
				t.Errorf("POST %s changed the repos page on refusal:\n%s", c.path, after)
			}
		})
	}
}

func TestDeleteProjectLeavesReposLoose(t *testing.T) {
	t.Parallel()
	server := projectsServer(t)
	form := url.Values{"name": {"acme"}, "hue": {"1"}, "repo": {"acme/alpha"}}
	do(t, server, http.MethodPost, "/projects", req{Form: form})

	if rec := do(t, server, http.MethodPost, "/projects/acme/delete", req{}); rec.Code != http.StatusSeeOther {
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
	do(t, server, http.MethodPost, "/projects", req{Form: url.Values{"name": {"acme"}, "hue": {"1"}}})

	move := url.Values{"project": {"acme"}}
	moveRec := do(t, server, http.MethodPost, "/repos/acme/solo/project", req{Form: move})
	if moveRec.Code != http.StatusSeeOther {
		t.Fatalf("move = %d: %s", moveRec.Code, moveRec.Body)
	}
	if body := renderPath(t, server, "/repos"); !strings.Contains(body, `data-scope="project"`) ||
		!strings.Contains(body, `<option value="acme" selected>`) {
		t.Errorf("GET /repos after move:\n%s", body)
	}

	loosen := url.Values{"project": {""}}
	loosenRec := do(t, server, http.MethodPost, "/repos/acme/solo/project", req{Form: loosen})
	if loosenRec.Code != http.StatusSeeOther {
		t.Fatalf("loosen = %d: %s", loosenRec.Code, loosenRec.Body)
	}
	if body := renderPath(t, server, "/repos"); strings.Contains(body, `<option value="acme" selected>`) {
		t.Errorf("GET /repos after loosen still selects acme:\n%s", body)
	}
}
