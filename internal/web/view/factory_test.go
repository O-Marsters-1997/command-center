package view_test

import (
	"net/url"
	"regexp"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

func scopeRows() []view.Row {
	return []view.Row{
		{Project: "beta", ProjectHue: 2, Feature: "project:zeta", Repo: "api", State: plan.PRMerged},
		{Project: "Acme", ProjectHue: 1, Feature: "project:alpha", Repo: "web", State: plan.PRMerged},
		{Project: "Acme", ProjectHue: 1, Feature: "project:alpha", Repo: "web"},
		{Project: "Acme", ProjectHue: 1, Feature: "project:Beta", Repo: "api"},
		{Project: "Acme", ProjectHue: 1, Repo: "api"},
		{Repo: "loose"},
	}
}

func TestBuildScopeBar(t *testing.T) {
	t.Parallel()

	acme := view.ProjectMark{Name: "Acme", Hue: 1}
	beta := view.ProjectMark{Name: "beta", Hue: 2}
	tests := []struct {
		name  string
		query string
		want  view.ScopeBar
	}{
		{
			name: "unscoped",
			want: view.ScopeBar{
				AllProjects: view.ScopeTab{Label: "All projects", Path: "/factory", Count: 4, Current: true},
				Projects: []view.ScopeTab{
					{Label: "Acme", Path: "/factory?project=Acme", Count: 3, Mark: acme},
					{Label: "beta", Path: "/factory?project=beta", Mark: beta},
				},
				Features: []view.ScopeTab{
					{Label: "alpha", Path: "/factory?feature=project%3Aalpha", Count: 1, Mark: acme},
					{Label: "Beta", Path: "/factory?feature=project%3ABeta", Count: 1, Mark: acme},
					{Label: "zeta", Path: "/factory?feature=project%3Azeta", Mark: beta},
				},
				Repos: []view.ScopeTab{
					{Label: "api", Path: "/factory?repo=api", Count: 2, Mark: beta},
					{Label: "loose", Path: "/factory?repo=loose", Count: 1},
					{Label: "web", Path: "/factory?repo=web", Count: 1, Mark: acme},
				},
			},
		},
		{
			name:  "project narrows features and repos, empty params dropped",
			query: "project=beta&feature=&closed=x",
			want: view.ScopeBar{
				AllProjects: view.ScopeTab{Label: "All projects", Path: "/factory", Count: 4},
				Projects: []view.ScopeTab{
					{Label: "Acme", Path: "/factory?project=Acme", Count: 3, Mark: acme},
					{Label: "beta", Path: "/factory", Current: true, Mark: beta},
				},
				Features: []view.ScopeTab{
					{Label: "zeta", Path: "/factory?closed=x&feature=project%3Azeta&project=beta", Mark: beta},
				},
				Repos: []view.ScopeTab{
					{Label: "api", Path: "/factory?closed=x&project=beta&repo=api", Mark: beta},
				},
			},
		},
		{
			name:  "current feature and repo each remove only their own param",
			query: "project=Acme&feature=project:alpha&repo=web",
			want: view.ScopeBar{
				AllProjects: view.ScopeTab{Label: "All projects", Path: "/factory", Count: 4},
				Projects: []view.ScopeTab{
					{Label: "Acme", Path: "/factory", Count: 3, Current: true, Mark: acme},
					{Label: "beta", Path: "/factory?project=beta", Mark: beta},
				},
				Features: []view.ScopeTab{
					{Label: "alpha", Path: "/factory?project=Acme&repo=web", Count: 1, Current: true, Mark: acme},
					{Label: "Beta", Path: "/factory?feature=project%3ABeta&project=Acme&repo=web", Count: 1, Mark: acme},
				},
				Repos: []view.ScopeTab{
					{Label: "api", Path: "/factory?feature=project%3Aalpha&project=Acme&repo=api", Count: 2, Mark: acme},
					{Label: "web", Path: "/factory?feature=project%3Aalpha&project=Acme", Count: 1, Current: true, Mark: acme},
				},
				Clear: "/factory?project=Acme",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			q, err := url.ParseQuery(tt.query)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tt.want, view.BuildScopeBar(scopeRows(), q)); diff != "" {
				t.Errorf("BuildScopeBar mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCard_TransitionName(t *testing.T) {
	t.Parallel()
	ident := regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_-]*$`)
	a := view.Card{Row: view.Row{URL: "sandbox://PLA-1"}}.TransitionName()
	b := view.Card{Row: view.Row{URL: "sandbox://PLA-2"}}.TransitionName()
	if a == b {
		t.Errorf("TransitionName() = %q for two tickets, want distinct", a)
	}
	for _, name := range []string{a, b} {
		if !ident.MatchString(name) {
			t.Errorf("TransitionName() = %q, want a CSS ident", name)
		}
	}
}
