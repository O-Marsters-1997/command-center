package web_test

import (
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/O-Marsters-1997/command-center/internal/htmltest"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
	"github.com/O-Marsters-1997/command-center/internal/web"
	"github.com/O-Marsters-1997/command-center/internal/web/webtest"
)

func threeRepoStore(t *testing.T) *storepkg.Store {
	t.Helper()
	return observedTickets(t,
		storepkg.Ticket{URL: "sandbox://ROOT", Repo: "repo", Branch: "root"},
		storepkg.Ticket{URL: "sandbox://CHILD", Repo: "repo", Branch: "child", BlockedBy: []string{"sandbox://ROOT"}},
		storepkg.Ticket{URL: "sandbox://LONE", Repo: "other", Branch: "lone"},
	)
}

func threeRepoServer(t *testing.T) *web.Server {
	t.Helper()
	return openServer(track(t, threeRepoStore(t), named("repo", "services", "other")...), fixedClock(testNow), "")
}

func threeFeatureStore(t *testing.T) *storepkg.Store {
	t.Helper()
	return observedTickets(t,
		storepkg.Ticket{URL: "sandbox://ROOT", Feature: "board-scope", Branch: "root"},
		storepkg.Ticket{
			URL: "sandbox://CHILD", Feature: "board-scope", Branch: "child", BlockedBy: []string{"sandbox://ROOT"},
		},
		storepkg.Ticket{URL: "sandbox://LONE", Feature: "sqlc-migration", Branch: "lone"},
	)
}

func threeFeatureServer(t *testing.T) *web.Server {
	t.Helper()
	return newServer(threeFeatureStore(t), testNow)
}

func boardRefs(t *testing.T, server *web.Server, path string) []string {
	t.Helper()
	return ticketRefs(webtest.BoardRows(t, renderPath(t, server, path)))
}

func spanTexts(n htmltest.Node) []string {
	var texts []string
	for _, span := range n.Find("span") {
		texts = append(texts, span.Text())
	}
	return texts
}

func testBoardScope(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, groupPath, otherPath string
		server                     func(*testing.T) *web.Server
	}{
		{"repo", "/board?repo=repo", "/board?repo=other", threeRepoServer},
		{"feature", "/f/board-scope", "/f/sqlc-migration", threeFeatureServer},
	} {
		t.Run(tt.name+" admits a group whole and drops an unrelated one", func(t *testing.T) {
			t.Parallel()
			server := tt.server(t)

			want := []string{ticketRef("sandbox://ROOT"), ticketRef("sandbox://CHILD")}
			if diff := cmp.Diff(want, boardRefs(t, server, tt.groupPath)); diff != "" {
				t.Errorf("GET %s refs mismatch (-want +got):\n%s", tt.groupPath, diff)
			}
			if diff := cmp.Diff([]string{ticketRef("sandbox://LONE")}, boardRefs(t, server, tt.otherPath)); diff != "" {
				t.Errorf("GET %s refs mismatch (-want +got):\n%s", tt.otherPath, diff)
			}
		})
	}

	t.Run("an out-of-scope group member names its own repo or feature", func(t *testing.T) {
		t.Parallel()
		for _, tt := range []struct {
			name, path, foreign, own string
			child                    storepkg.Ticket
			root                     storepkg.Ticket
		}{
			{
				name: "feature", path: "/f/board-scope", foreign: "sqlc-migration", own: "board-scope",
				root: storepkg.Ticket{URL: "sandbox://ROOT", Feature: "board-scope", Branch: "root"},
				child: storepkg.Ticket{
					URL: "sandbox://CHILD", Feature: "sqlc-migration", Branch: "child", BlockedBy: []string{"sandbox://ROOT"},
				},
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				store := observedTickets(t, tt.root, tt.child)
				page := renderPath(t, openServer(track(t, store, named("repo", "services")...), fixedClock(testNow), ""), tt.path)

				if got := spanTexts(webtest.RowFor(t, page, tt.child.URL).Row); !slices.Contains(got, tt.foreign) {
					t.Errorf("GET %s CHILD spans = %q, want it to name %s", tt.path, got, tt.foreign)
				}
				if got := spanTexts(webtest.RowFor(t, page, tt.root.URL).Row); slices.Contains(got, tt.own) {
					t.Errorf("GET %s ROOT spans = %q, want the in-scope %s left unnamed", tt.path, got, tt.own)
				}
			})
		}
	})

	t.Run("a repo scope accepts an owner/name with its slash", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		storetest.Tickets(t, store,
			storepkg.Ticket{URL: "sandbox://MINE", Repo: "O-Marsters-1997/command-center", Branch: "mine"},
			storepkg.Ticket{URL: "sandbox://THEIRS", Repo: "acme/other", Branch: "theirs"},
		)
		tracked := track(t, store, named("O-Marsters-1997/command-center", "acme/other")...)
		server := openServer(tracked, fixedClock(testNow), "")

		refs := boardRefs(t, server, "/board?repo=O-Marsters-1997/command-center")
		if diff := cmp.Diff([]string{ticketRef("sandbox://MINE")}, refs); diff != "" {
			t.Errorf("GET /board?repo=O-Marsters-1997/command-center refs mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("an unknown repo scope falls back to unscoped", func(t *testing.T) {
		t.Parallel()
		want := []string{ticketRef("sandbox://ROOT"), ticketRef("sandbox://CHILD"), ticketRef("sandbox://LONE")}
		got := boardRefs(t, threeRepoServer(t), "/board?repo=bogus")
		if diff := cmp.Diff(want, got, sortStrings); diff != "" {
			t.Errorf("GET /board?repo=bogus refs mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("feature and repo scopes compose", func(t *testing.T) {
		t.Parallel()
		store := observedTickets(t,
			storepkg.Ticket{URL: "sandbox://MATCH", Repo: "repo", Feature: "board-scope", Branch: "match"},
			storepkg.Ticket{URL: "sandbox://WRONG-REPO", Repo: "other", Feature: "board-scope", Branch: "wrong-repo"},
			storepkg.Ticket{URL: "sandbox://WRONG-FEATURE", Repo: "repo", Feature: "sqlc-migration", Branch: "wrong-feature"},
		)
		server := openServer(track(t, store, named("repo", "other")...), fixedClock(testNow), "")

		refs := boardRefs(t, server, "/board?feature=board-scope&repo=repo")
		if diff := cmp.Diff([]string{ticketRef("sandbox://MATCH")}, refs); diff != "" {
			t.Errorf("GET /board?feature=board-scope&repo=repo refs mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("clearing one scope axis leaves the other applied", func(t *testing.T) {
		t.Parallel()
		want := []string{ticketRef("sandbox://ROOT"), ticketRef("sandbox://CHILD")}
		if diff := cmp.Diff(want, boardRefs(t, threeRepoServer(t), "/board?repo=repo&feature=")); diff != "" {
			t.Errorf("GET /board?repo=repo&feature= refs mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("a repo scope survives the board's own poll and row paths", func(t *testing.T) {
		t.Parallel()
		page := renderPath(t, threeRepoServer(t), "/board?repo=repo")

		if got, _ := htmltest.Parse(t, page).One(t, "table#board").Attr("hx-get"); got != "/board?repo=repo" {
			t.Errorf("GET /board?repo=repo poll hx-get = %q, want /board?repo=repo", got)
		}
		selection := webtest.RowFor(t, page, "sandbox://ROOT").Row.One(t, `input[name="ticket"]`)
		if got, _ := selection.Attr("hx-get"); !strings.Contains(got, "repo=repo") {
			t.Errorf("GET /board?repo=repo ROOT selection hx-get = %q, want it to keep repo=repo", got)
		}
	})

	t.Run("a project scope keeps a group whole and carries it in every link", func(t *testing.T) {
		t.Parallel()
		st := track(t, openStore(t), alphaRepo, betaRepo, soloRepo)
		storetest.Tickets(t, st,
			storepkg.Ticket{URL: "https://github.com/acme/solo/issues/3", Repo: "acme/solo", Branch: "s-3"},
			storepkg.Ticket{URL: "https://github.com/acme/alpha/issues/1", Repo: "acme/alpha", Branch: "a-1",
				BlockedBy: []string{"https://github.com/acme/solo/issues/3"}},
			storepkg.Ticket{URL: "https://github.com/acme/solo/issues/4", Repo: "acme/solo", Branch: "s-4"},
		)
		if err := st.CreateProject(t.Context(), storepkg.NewProject{
			Name: "acme", Hue: 1, Repos: []string{"acme/alpha", "acme/beta"}, At: testNow,
		}); err != nil {
			t.Fatalf("CreateProject: %v", err)
		}
		server := openServer(st, fixedClock(testNow), "")

		root := htmltest.Parse(t, renderPath(t, server, "/tickets?project=ACME"))
		for _, href := range []string{
			"/s/acme/alpha/1?project=acme", "/tickets?project=acme", "/insights?project=acme",
			"/repos?project=acme", "/launch?project=acme",
		} {
			if links := root.Find(`a[href="` + href + `"]`); len(links) == 0 {
				t.Errorf("GET /tickets?project=ACME has no link to %s", href)
			}
		}
		board := root.One(t, "table#board")
		if get, _ := board.Attr("hx-get"); get != "/board?all=1&project=acme" {
			t.Errorf("GET /tickets?project=ACME board hx-get = %q, want /board?all=1&project=acme", get)
		}
		if got := board.One(t, `tr.group [data-hue="1"][aria-hidden="true"]`).Text(); got != "A" {
			t.Errorf("GET /tickets?project=ACME avatar = %q, want A", got)
		}
		if others := board.Find(`a[href="/s/acme/solo/4"]`); len(others) != 0 {
			t.Errorf("GET /tickets?project=ACME includes the unrelated ticket: %s", others[0].HTML())
		}

		unknown := renderPath(t, server, "/tickets?project="+url.QueryEscape("nope"))
		webtest.RowFor(t, unknown, "https://github.com/acme/solo/issues/4")
		if get, _ := htmltest.Parse(t, unknown).One(t, "table#board").Attr("hx-get"); get != "/board?all=1" {
			t.Errorf("GET /tickets?project=nope board hx-get = %q, want /board?all=1", get)
		}
	})
}

var sortStrings = cmpopts.SortSlices(func(a, b string) bool { return a < b })
