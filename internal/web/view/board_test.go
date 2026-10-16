package view_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

func groupURLs(groups []view.Group) [][]string {
	out := [][]string{}
	for _, g := range groups {
		var urls []string
		if g.Root.IsSome() {
			urls = append(urls, g.Root.Unwrap().URL)
		}
		for _, child := range g.Children {
			urls = append(urls, child.URL)
		}
		out = append(out, urls)
	}
	return out
}

func TestFilterGroupsByRepo(t *testing.T) {
	t.Parallel()

	groups := []view.Group{
		{Root: option.Some(view.Row{URL: "root", Repo: "repo"}), Children: []view.Row{{URL: "child", Repo: "services"}}},
		{Children: []view.Row{{URL: "unrelated", Repo: "other"}}},
	}
	tests := []struct {
		repo string
		want [][]string
	}{
		{repo: "", want: [][]string{{"root", "child"}, {"unrelated"}}},
		{repo: "repo", want: [][]string{{"root", "child"}}},
		{repo: "services", want: [][]string{{"root", "child"}}},
		{repo: "other", want: [][]string{{"unrelated"}}},
		{repo: "absent", want: [][]string{}},
	}
	for _, tt := range tests {
		t.Run(tt.repo, func(t *testing.T) {
			t.Parallel()
			if diff := cmp.Diff(tt.want, groupURLs(view.FilterGroupsByRepo(groups, tt.repo))); diff != "" {
				t.Errorf("FilterGroupsByRepo(%q) mismatch (-want +got):\n%s", tt.repo, diff)
			}
		})
	}
}

func TestFilterGroupsByFeature(t *testing.T) {
	t.Parallel()

	groups := []view.Group{
		{
			Root:     option.Some(view.Row{URL: "root", Repo: "repo", Feature: "board-scope"}),
			Children: []view.Row{{URL: "child", Repo: "services", Feature: "sqlc-migration"}},
		},
		{Children: []view.Row{{URL: "unrelated", Repo: "repo", Feature: "other"}}},
	}
	tests := []struct {
		name    string
		feature string
		want    [][]string
	}{
		{name: "blank is unscoped", want: [][]string{{"root", "child"}, {"unrelated"}}},
		{name: "a root admits its group whole", feature: "board-scope", want: [][]string{{"root", "child"}}},
		{name: "a child admits its group whole", feature: "sqlc-migration", want: [][]string{{"root", "child"}}},
		{name: "an unrelated group alone", feature: "other", want: [][]string{{"unrelated"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if diff := cmp.Diff(tt.want, groupURLs(view.FilterGroupsByFeature(groups, tt.feature))); diff != "" {
				t.Errorf("FilterGroupsByFeature(%q) mismatch (-want +got):\n%s", tt.feature, diff)
			}
		})
	}

	t.Run("composes with the repo filter, neither overriding the other", func(t *testing.T) {
		t.Parallel()
		flat := []view.Group{
			{Children: []view.Row{{URL: "root", Repo: "repo", Feature: "board-scope"}}},
			{Children: []view.Row{{URL: "other", Repo: "services", Feature: "sqlc-migration"}}},
		}
		for feature, want := range map[string][][]string{"board-scope": {{"root"}}, "sqlc-migration": {}} {
			got := groupURLs(view.FilterGroupsByFeature(view.FilterGroupsByRepo(flat, "repo"), feature))
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("FilterGroupsByFeature(FilterGroupsByRepo(repo), %q) mismatch (-want +got):\n%s", feature, diff)
			}
		}
	})
}

func TestFilterGroupsByProject(t *testing.T) {
	t.Parallel()

	web := view.Row{URL: "web", Repo: "acme/web"}
	api := view.Row{URL: "api", Repo: "acme/api"}
	elsewhere := view.Row{URL: "elsewhere", Repo: "other/repo"}
	groups := []view.Group{
		{Root: option.Some(web), Children: []view.Row{elsewhere}},
		{Root: option.Some(elsewhere), Children: []view.Row{api}},
		{Root: option.Some(elsewhere), Children: []view.Row{{URL: "x", Repo: "other/repo"}}},
	}

	got := groupURLs(view.FilterGroupsByProject(groups, []string{"acme/web", "acme/api"}))
	want := [][]string{{"web", "elsewhere"}, {"elsewhere", "api"}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("FilterGroupsByProject(acme) mismatch (-want +got):\n%s", diff)
	}
}

func TestDetailOf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, reason, want string
	}{
		{"one blocker", "blocked by sandbox://PLA-40, not yet merged", "blocked by #PLA-40, not yet merged"},
		{
			"an issue url",
			"waiting on https://github.com/o/r/issues/12: its pull request was closed",
			"waiting on #12: its pull request was closed",
		},
		{"several blockers", "waiting on 2 blockers: sandbox://A-1, sandbox://A-2", "waiting on 2 blockers: #A-1, #A-2"},
		{
			"a log path",
			"no commits after this run's baseline; log at /home/o/.cc/runs/7.log",
			"no commits after this run's baseline",
		},
		{"plain prose", "Which database?", "Which database?"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := view.DetailOf(tt.reason); got != tt.want {
				t.Errorf("DetailOf(%q) = %q, want %q", tt.reason, got, tt.want)
			}
		})
	}
}
