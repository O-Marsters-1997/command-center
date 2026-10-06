package view

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/config"
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

	tickets := []store.Ticket{{Feature: "sqlc-migration"}, {Feature: "board-scope"}, {Feature: ""}, {Feature: "board-scope"}}
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

func TestRepoLinksForIsNilWithNoConfiguredRepos(t *testing.T) {
	t.Parallel()

	if got := repoLinksFor(nil, Params{}); got != nil {
		t.Errorf("repoLinksFor(nil, ...) = %+v, want nil", got)
	}
}

func TestRepoLinksForNamesAllPlusEveryConfiguredRepo(t *testing.T) {
	t.Parallel()

	repos := []config.Repo{{Name: "repo"}, {Name: "services"}}
	got := repoLinksFor(repos, Params{Repo: "services"})
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
