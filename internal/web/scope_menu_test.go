package web_test

import (
	"strings"
	"testing"
)

func TestScopeMenuListsProjectsThenLooseReposWithKeys(t *testing.T) {
	t.Parallel()
	st := track(t, openStore(t), alphaRepo, betaRepo, soloRepo)
	if err := st.CreateProject(t.Context(), "acme", 1, []string{"acme/alpha", "acme/beta"}, testNow); err != nil {
		t.Fatal(err)
	}
	server := openServer(st, fixedClock(testNow), "")

	body := renderPath(t, server, "/tickets")
	var last int
	for _, want := range []string{
		`data-scope-menu`, `All projects · 0 running`,
		`href="/tickets" data-key="g 0"`,
		`href="/tickets?project=acme" data-key="g 1"`,
		`href="/tickets?repo=acme%2Fsolo" data-key="g 2"`,
	} {
		i := strings.Index(body, want)
		if i < 0 {
			t.Fatalf("GET /tickets missing %q:\n%s", want, body)
		}
		if i < last {
			t.Errorf("GET /tickets has %q out of order", want)
		}
		last = i
	}
	if strings.Contains(body, `data-key="g 3"`) {
		t.Errorf("GET /tickets gave a third entry a key")
	}
	if strings.Index(body, `data-scope-menu`) > strings.Index(body, `id="rail-search"`) {
		t.Errorf("GET /tickets put the scope menu below the search box")
	}
}

func TestScopeMenuSummaryNamesTheScope(t *testing.T) {
	t.Parallel()
	st := track(t, openStore(t), alphaRepo, soloRepo)
	if err := st.CreateProject(t.Context(), "acme", 1, []string{"acme/alpha"}, testNow); err != nil {
		t.Fatal(err)
	}
	server := openServer(st, fixedClock(testNow), "")

	project := renderPath(t, server, "/tickets?project=acme")
	if !strings.Contains(project, `aria-hidden="true">A</span> acme</summary>`) {
		t.Errorf("GET /tickets?project=acme summary does not name the project:\n%s", project)
	}
	repo := renderPath(t, server, "/tickets?repo=acme%2Fsolo")
	if !strings.Contains(repo, `px-2">acme/solo</summary>`) {
		t.Errorf("GET /tickets?repo=acme/solo summary does not name the repo:\n%s", repo)
	}
}
