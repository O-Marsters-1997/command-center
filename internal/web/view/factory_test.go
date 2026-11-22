package view_test

import (
	"net/url"
	"regexp"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
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

func inStage(stamp string) option.Option[view.Age] {
	return option.Some(view.Age{Age: "1h", Stamp: stamp})
}

func floorColumn(t *testing.T, columns []view.Column, stage plan.Stage) view.Column {
	t.Helper()
	for _, c := range columns {
		if c.Stage == stage {
			return c
		}
	}
	t.Fatalf("no %s column", stage)
	return view.Column{}
}

func floorColumns(rows []view.Row) []view.Column {
	lanes, _ := view.BuildFloor(rows, view.FactoryParams{})
	return lanes[0].Columns
}

func TestBuildFloor(t *testing.T) {
	t.Parallel()

	t.Run("flagged cards sort first and review cards are never flagged", func(t *testing.T) {
		t.Parallel()
		rows := []view.Row{
			{URL: "sandbox://A-1", Stage: plan.StageWorking, Glyph: plan.GlyphRunning},
			{URL: "sandbox://A-2", Stage: plan.StageWorking, Glyph: plan.GlyphFailed},
			{URL: "sandbox://R-1", Stage: plan.StageReview, Glyph: plan.GlyphAttention},
		}
		columns := floorColumns(rows)

		var order []string
		for _, c := range floorColumn(t, columns, plan.StageWorking).Cards {
			order = append(order, c.URL)
		}
		if diff := cmp.Diff([]string{"sandbox://A-2", "sandbox://A-1"}, order); diff != "" {
			t.Errorf("Agent working order mismatch (-want +got):\n%s", diff)
		}
		if review := floorColumn(t, columns, plan.StageReview); review.Cards[0].Flagged {
			t.Errorf("review card flagged = true, want false")
		}
	})

	t.Run("review reports its oldest card and pressure at three", func(t *testing.T) {
		t.Parallel()
		review := func(url, stamp string) view.Row {
			return view.Row{URL: url, Stage: plan.StageReview, Glyph: plan.GlyphAttention, InStage: inStage(stamp)}
		}
		two := []view.Row{
			review("sandbox://R-1", "2026-01-02T00:00:00Z"), review("sandbox://R-2", "2026-01-01T00:00:00Z"),
		}
		columns := floorColumns(two)
		got := floorColumn(t, columns, plan.StageReview)
		if got.Pressure {
			t.Errorf("two review cards: Pressure = true, want false")
		}
		if want := inStage("2026-01-01T00:00:00Z"); got.Oldest != want {
			t.Errorf("Oldest = %v, want %v", got.Oldest, want)
		}

		columns = floorColumns(append(two, review("sandbox://R-3", "2026-01-03T00:00:00Z")))
		if got := floorColumn(t, columns, plan.StageReview); !got.Pressure {
			t.Errorf("three review cards: Pressure = false, want true")
		}
		if got := floorColumn(t, columns, plan.StageWorking); got.Pressure || got.Oldest.IsSome() {
			t.Errorf("working column carries review pressure: %+v", got)
		}
	})
}
