package web_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/htmltest"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

func testLayoutScopeMenu(t *testing.T) {
	t.Parallel()

	t.Run("lists projects then loose repos with keys", func(t *testing.T) {
		t.Parallel()
		st := track(t, openStore(t), alphaRepo, betaRepo, soloRepo)
		if err := st.CreateProject(t.Context(), storepkg.NewProject{
			Name: "acme", Hue: 1, Repos: []string{"acme/alpha", "acme/beta"}, At: testNow,
		}); err != nil {
			t.Fatalf("CreateProject(acme): %v", err)
		}

		page := renderPath(t, openServer(st, fixedClock(testNow), ""), "/tickets")
		menu := htmltest.Parse(t, page).One(t, "[data-scope-menu]")
		if got := menu.One(t, "summary").Text(); !strings.Contains(got, "All projects") {
			t.Errorf("GET /tickets scope summary = %q, want All projects", got)
		}
		var got []string
		for _, link := range menu.Find("nav a") {
			a := attrs(link, "href", "data-key")
			got = append(got, a["href"]+" "+a["data-key"])
		}
		want := []string{"/tickets g 0", "/tickets?project=acme g 1", "/tickets?repo=acme%2Fsolo g 2"}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("GET /tickets scope links mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("the summary names the scope", func(t *testing.T) {
		t.Parallel()
		st := track(t, openStore(t), alphaRepo, soloRepo)
		if err := st.CreateProject(t.Context(), storepkg.NewProject{
			Name: "acme", Hue: 1, Repos: []string{"acme/alpha"}, At: testNow,
		}); err != nil {
			t.Fatalf("CreateProject(acme): %v", err)
		}
		server := openServer(st, fixedClock(testNow), "")

		project := htmltest.Parse(t, renderPath(t, server, "/tickets?project=acme")).One(t, "[data-scope-menu] summary")
		project.One(t, `[data-hue][aria-hidden="true"]`)
		if got := project.Text(); !strings.Contains(got, "acme") {
			t.Errorf("GET /tickets?project=acme scope summary = %q, want acme", got)
		}
		repo := htmltest.Parse(t, renderPath(t, server, "/tickets?repo=acme%2Fsolo")).One(t, "[data-scope-menu] summary")
		if got := repo.Text(); !strings.Contains(got, "acme/solo") {
			t.Errorf("GET /tickets?repo=acme/solo scope summary = %q, want acme/solo", got)
		}
	})
}
