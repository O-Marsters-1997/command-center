package cc

import (
	"bytes"
	"strings"
	"testing"
)

func TestFilterGroupsByRepoIsUnscopedWhenRepoIsBlank(t *testing.T) {
	t.Parallel()

	groups := []group{{Children: []row{{URL: "a", Repo: "repo"}}}}
	if got := filterGroupsByRepo(groups, ""); len(got) != 1 {
		t.Errorf("filterGroupsByRepo(groups, \"\") = %+v, want groups unchanged", got)
	}
}

func TestFilterGroupsByRepoAdmitsAGroupWholeOnEitherMember(t *testing.T) {
	t.Parallel()

	root := row{URL: "root", Repo: "repo"}
	child := row{URL: "child", Repo: "services"}
	groups := []group{
		{Root: &root, Children: []row{child}},
		{Children: []row{{URL: "unrelated", Repo: "other"}}},
	}

	forRepo := filterGroupsByRepo(groups, "repo")
	if len(forRepo) != 1 || forRepo[0].Root.URL != "root" || len(forRepo[0].Children) != 1 {
		t.Fatalf("filterGroupsByRepo(groups, \"repo\") = %+v, want the cross-repo group whole", forRepo)
	}

	forServices := filterGroupsByRepo(groups, "services")
	if len(forServices) != 1 || forServices[0].Root.URL != "root" {
		t.Fatalf("filterGroupsByRepo(groups, \"services\") = %+v, want the same group admitted from its other member",
			forServices)
	}

	forOther := filterGroupsByRepo(groups, "other")
	if len(forOther) != 1 || forOther[0].Root != nil || forOther[0].Children[0].URL != "unrelated" {
		t.Fatalf("filterGroupsByRepo(groups, \"other\") = %+v, want only the unrelated group", forOther)
	}
}

func TestRowsInFlattensRootAndChildren(t *testing.T) {
	t.Parallel()

	root := row{URL: "root"}
	groups := []group{
		{Root: &root, Children: []row{{URL: "child"}}},
		{Children: []row{{URL: "lone"}}},
	}
	got := rowsIn(groups)
	if len(got) != 3 {
		t.Fatalf("rowsIn(groups) = %+v, want 3 rows", got)
	}
}

func TestRepoLinksForIsNilWithNoConfiguredRepos(t *testing.T) {
	t.Parallel()

	if got := repoLinksFor(nil, viewParams{}); got != nil {
		t.Errorf("repoLinksFor(nil, ...) = %+v, want nil", got)
	}
}

// TestBoardNamesAnOutOfScopeGroupMembersOwnRepo covers issue #219 AC2 at the template's own
// seam: plan.Unlocked only ever counts a same-repo blocker (plan.go:60), so a group groupRows
// forms can never itself straddle two repos through today's live blocking edges -- this drives
// board.tmpl's row template directly, over a hand-built group, the way ADR 11 describes one.
func TestBoardNamesAnOutOfScopeGroupMembersOwnRepo(t *testing.T) {
	t.Parallel()

	root := row{URL: "sandbox://ROOT", Repo: "repo", State: "ready", Tone: "idle"}
	child := row{URL: "sandbox://CHILD", Repo: "services", State: "ready", Tone: "idle", Blocking: []string{root.URL}}
	view := pageView{
		Groups: []group{{Root: &root, Children: []row{child}}}, RepoScope: "repo", BoardPath: "/board?repo=repo",
	}

	var buf bytes.Buffer
	if err := boardFragment.Execute(&buf, view); err != nil {
		t.Fatal(err)
	}
	html := buf.String()

	if !strings.Contains(html, ">services<") {
		t.Errorf("board did not name CHILD's own repo though it is out of the repo scope:\n%s", html)
	}
	rootRow := html[:strings.Index(html, "#CHILD")]
	if strings.Contains(rootRow, ">repo<") {
		t.Errorf("board named ROOT's own repo though it matches the scope:\n%s", rootRow)
	}
}

func TestRepoLinksForNamesAllPlusEveryConfiguredRepo(t *testing.T) {
	t.Parallel()

	repos := []Repo{{Name: "repo"}, {Name: "services"}}
	got := repoLinksFor(repos, viewParams{Repo: "services"})
	if len(got) != 3 {
		t.Fatalf("repoLinksFor(repos, {Repo: services}) = %+v, want 3 links", got)
	}
	if got[0].Name != "all" || got[0].Current {
		t.Errorf("all link = %+v, want Current=false since a repo is scoped", got[0])
	}
	if got[1].Name != "repo" || got[1].Current {
		t.Errorf("repo link = %+v, want Current=false", got[1])
	}
	if got[2].Name != "services" || !got[2].Current || got[2].Path != "/?repo=services" {
		t.Errorf("services link = %+v, want Current=true and Path=/?repo=services", got[2])
	}
}
