package view

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/store"
)

func TestFilterGroupsByRepoIsUnscopedWhenRepoIsBlank(t *testing.T) {
	t.Parallel()

	groups := []Group{{Children: []Row{{URL: "a", Repo: "repo"}}}}
	if got := filterGroupsByRepo(groups, ""); len(got) != 1 {
		t.Errorf("filterGroupsByRepo(groups, \"\") = %+v, want groups unchanged", got)
	}
}

func TestFilterGroupsByRepoAdmitsAGroupWholeOnEitherMember(t *testing.T) {
	t.Parallel()

	root := Row{URL: "root", Repo: "repo"}
	child := Row{URL: "child", Repo: "services"}
	groups := []Group{
		{Root: &root, Children: []Row{child}},
		{Children: []Row{{URL: "unrelated", Repo: "other"}}},
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

func TestFilterGroupsByProjectAdmitsAGroupWholeOnAnyMember(t *testing.T) {
	t.Parallel()

	web := Row{URL: "web", Repo: "acme/web"}
	api := Row{URL: "api", Repo: "acme/api"}
	elsewhere := Row{URL: "elsewhere", Repo: "other/repo"}
	groups := []Group{
		{Root: &web, Children: []Row{elsewhere}},
		{Root: &elsewhere, Children: []Row{api}},
		{Root: &elsewhere, Children: []Row{{URL: "x", Repo: "other/repo"}}},
	}

	got := filterGroupsByProject(groups, []string{"acme/web", "acme/api"})
	if len(got) != 2 {
		t.Fatalf("filterGroupsByProject(groups, acme) = %+v, want the two groups with an acme member", got)
	}
	if got[0].Root.URL != "web" || len(got[0].Children) != 1 || got[0].Children[0].URL != "elsewhere" {
		t.Errorf("group rooted in acme = %+v, want it whole with its other-project child", got[0])
	}
	if got[1].Root.URL != "elsewhere" || got[1].Children[0].URL != "api" {
		t.Errorf("group with an acme child = %+v, want it admitted whole", got[1])
	}
}

func TestApplyViewStateCarriesTheProjectOnRowLinks(t *testing.T) {
	t.Parallel()

	rows := []Row{{URL: "https://github.com/acme/web/issues/7"}}
	if err := applyViewState(rows, Params{Project: "acme"}, nil, nil); err != nil {
		t.Fatal(err)
	}

	if want := "/s/acme/web/7?project=acme"; rows[0].SessionPath != want {
		t.Errorf("SessionPath = %q, want %q", rows[0].SessionPath, want)
	}
	if want := "/verb?project=acme"; rows[0].VerbPath != want {
		t.Errorf("VerbPath = %q, want %q", rows[0].VerbPath, want)
	}
}

func TestNormalizeProjectScopeIgnoresCase(t *testing.T) {
	t.Parallel()

	projects := []store.Project{{Name: "acme", Hue: 2}}
	if got := normalizeProjectScope("ACME", projects); got.Name != "acme" {
		t.Errorf("normalizeProjectScope(ACME) = %+v, want acme", got)
	}
	if got := normalizeProjectScope("nope", projects); got.Name != "" {
		t.Errorf("normalizeProjectScope(nope) = %+v, want no project", got)
	}
}

func TestFilterGroupsByFeatureIsUnscopedWhenFeatureIsBlank(t *testing.T) {
	t.Parallel()

	groups := []Group{{Children: []Row{{URL: "a", Feature: "board-scope"}}}}
	if got := filterGroupsByFeature(groups, ""); len(got) != 1 {
		t.Errorf("filterGroupsByFeature(groups, \"\") = %+v, want groups unchanged", got)
	}
}

func TestFilterGroupsByFeatureAdmitsAGroupWholeOnEitherMember(t *testing.T) {
	t.Parallel()

	root := Row{URL: "root", Feature: "board-scope"}
	child := Row{URL: "child", Feature: "sqlc-migration"}
	groups := []Group{
		{Root: &root, Children: []Row{child}},
		{Children: []Row{{URL: "unrelated", Feature: "other"}}},
	}

	forBoardScope := filterGroupsByFeature(groups, "board-scope")
	if len(forBoardScope) != 1 || forBoardScope[0].Root.URL != "root" || len(forBoardScope[0].Children) != 1 {
		t.Fatalf("filterGroupsByFeature(groups, \"board-scope\") = %+v, want the cross-feature group whole",
			forBoardScope)
	}

	forSqlcMigration := filterGroupsByFeature(groups, "sqlc-migration")
	if len(forSqlcMigration) != 1 || forSqlcMigration[0].Root.URL != "root" {
		t.Fatalf("filterGroupsByFeature(groups, \"sqlc-migration\") = %+v, want the same group admitted from "+
			"its other member", forSqlcMigration)
	}

	forOther := filterGroupsByFeature(groups, "other")
	if len(forOther) != 1 || forOther[0].Root != nil || forOther[0].Children[0].URL != "unrelated" {
		t.Fatalf("filterGroupsByFeature(groups, \"other\") = %+v, want only the unrelated group", forOther)
	}
}

func TestFilterGroupsComposeRepoAndFeatureNeitherOverridingTheOther(t *testing.T) {
	t.Parallel()

	root := Row{URL: "root", Repo: "repo", Feature: "board-scope"}
	other := Row{URL: "other", Repo: "services", Feature: "sqlc-migration"}
	groups := []Group{{Children: []Row{root}}, {Children: []Row{other}}}

	got := filterGroupsByFeature(filterGroupsByRepo(groups, "repo"), "board-scope")
	if len(got) != 1 || got[0].Children[0].URL != "root" {
		t.Fatalf("filterGroupsByFeature(filterGroupsByRepo(groups, repo), board-scope) = %+v, want only root", got)
	}

	dropped := filterGroupsByFeature(filterGroupsByRepo(groups, "repo"), "sqlc-migration")
	if len(dropped) != 0 {
		t.Fatalf("filterGroupsByFeature(filterGroupsByRepo(groups, repo), sqlc-migration) = %+v, want none: "+
			"root matches repo but not feature", dropped)
	}
}

func TestDistinctFeaturesSortsAndDropsBlank(t *testing.T) {
	t.Parallel()

	tickets := []store.Ticket{
		{Feature: "sqlc-migration"}, {Feature: "board-scope"}, {Feature: ""}, {Feature: "board-scope"},
	}
	got := distinctFeatures(tickets)
	want := []string{"board-scope", "sqlc-migration"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("distinctFeatures(tickets) = %v, want %v", got, want)
	}
}

func TestRowsInFlattensRootAndChildren(t *testing.T) {
	t.Parallel()

	root := Row{URL: "root"}
	groups := []Group{
		{Root: &root, Children: []Row{{URL: "child"}}},
		{Children: []Row{{URL: "lone"}}},
	}
	got := rowsIn(groups)
	if len(got) != 3 {
		t.Fatalf("rowsIn(groups) = %+v, want 3 rows", got)
	}
}
