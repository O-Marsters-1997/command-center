package web_test

import (
	"net/url"
	"strings"
	"testing"

	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

func TestProjectScopeKeepsAGroupWholeAndCarriesItInEveryLink(t *testing.T) {
	t.Parallel()
	st := track(t, openStore(t), alphaRepo, betaRepo, soloRepo)
	tickets := []storepkg.Ticket{
		{URL: "https://github.com/acme/solo/issues/3", Repo: "acme/solo", Branch: "s-3"},
		{URL: "https://github.com/acme/alpha/issues/1", Repo: "acme/alpha", Branch: "a-1",
			BlockedBy: []string{"https://github.com/acme/solo/issues/3"}},
		{URL: "https://github.com/acme/solo/issues/4", Repo: "acme/solo", Branch: "s-4"},
	}
	if err := st.UpsertTickets(t.Context(), tickets); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateProject(t.Context(), storepkg.NewProject{
		Name: "acme", Hue: 1, Repos: []string{"acme/alpha", "acme/beta"}, At: testNow,
	}); err != nil {
		t.Fatal(err)
	}
	server := openServer(st, fixedClock(testNow), "")

	body := renderPath(t, server, "/tickets?project=ACME")
	for _, want := range []string{
		`href="/s/acme/alpha/1?project=acme"`,
		`href="/tickets?project=acme"`, `href="/insights?project=acme"`,
		`href="/repos?project=acme"`, `href="/launch?project=acme"`,
		`hx-get="/board?all=1&amp;project=acme"`, `class="avatar" data-hue="1" aria-hidden="true">A</span> acme`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("GET /tickets?project=ACME missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "/s/acme/solo/4") {
		t.Errorf("GET /tickets?project=ACME listed a group with no acme member:\n%s", body)
	}

	unknown := renderPath(t, server, "/tickets?project="+url.QueryEscape("nope"))
	if !strings.Contains(unknown, "/s/acme/solo/4") || strings.Contains(unknown, "project=nope") {
		t.Errorf("GET /tickets?project=nope want the unscoped board:\n%s", unknown)
	}
}
