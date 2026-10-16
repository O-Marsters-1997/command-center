package view_test

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

var testNow = time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

func railRow(n, feature string, glyph plan.GlyphWord) view.Row {
	return view.Row{
		URL: "sandbox://" + n, Repo: "repo", Feature: feature, Glyph: glyph, Verbs: []plan.Verb{plan.VerbReRun},
	}
}

func refs(items []view.RailItem) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.Ref)
	}
	return out
}

func liveByFeature(rail view.Rail) map[string][]string {
	live := map[string][]string{}
	for _, f := range rail.Features {
		live[f.Name] = refs(f.Live)
	}
	return live
}

func scopedRailRows() []view.Row {
	return []view.Row{
		{URL: "https://github.com/acme/web/issues/1", Repo: "acme/web", Feature: "checkout", Glyph: plan.GlyphFailed},
		{URL: "https://github.com/acme/api/issues/2", Repo: "acme/api", Feature: "checkout", Glyph: plan.GlyphRunning},
		{URL: "https://github.com/other/repo/issues/3", Repo: "other/repo", Feature: "billing", Glyph: plan.GlyphFailed},
		{URL: "https://github.com/other/repo/issues/4", Repo: "other/repo", Feature: "billing", Glyph: plan.GlyphRunning},
		{
			URL: "https://github.com/other/repo/issues/5", Repo: "other/repo", Feature: "billing",
			Glyph: plan.GlyphAttention, Blocking: []string{"https://github.com/acme/web/issues/1"},
		},
	}
}

func searchRows() []view.Row {
	rows := []view.Row{
		railRow("12", "billing", plan.GlyphFailed),
		railRow("2", "billing", plan.GlyphRunning),
		railRow("3", "billing", plan.GlyphRunning),
		railRow("40", "checkout", plan.GlyphRunning),
		railRow("5", "checkout", plan.GlyphDone),
	}
	rows[0].Title = "Fix invoice rounding"
	rows[1].Title = "Add tax lines"
	rows[2].Title = "Refund flow"
	rows[3].Title = "Cart totals"
	rows[4].Title = "Old thing"
	rows[4].Worktree = "/wt"
	rows[3].Repo = "web"
	return rows
}

var acmeScope = view.RailParams{Project: "acme", ProjectRepos: []string{"acme/web", "acme/api"}}

func TestBuildRail(t *testing.T) {
	t.Parallel()

	t.Run("groups by attention", func(t *testing.T) {
		t.Parallel()
		rows := []view.Row{
			railRow("12", "billing", plan.GlyphAttention),
			railRow("1", "billing", plan.GlyphFailed),
			railRow("2", "billing", plan.GlyphRunning),
			railRow("6", "billing", plan.GlyphPending),
			railRow("11", "billing", plan.GlyphChecking),
			railRow("7", "billing", plan.GlyphReady),
			railRow("8", "billing", plan.GlyphBlocked),
			railRow("9", "billing", plan.GlyphBlocked),
			railRow("5", "billing", plan.GlyphDone),
			railRow("20", "idle", plan.GlyphReady),
			railRow("21", "idle", plan.GlyphDone),
			railRow("30", "", plan.GlyphRunning),
		}
		rows[len(rows)-3].Worktree = "/wt"
		rows[8].Worktree = "/wt"

		rail := view.BuildRail(rows, testNow, view.RailParams{Open: []string{"feature:billing"}})

		if diff := cmp.Diff([]string{"#1", "#12"}, refs(rail.NeedsYou)); diff != "" {
			t.Errorf("BuildRail().NeedsYou mismatch (-want +got):\n%s", diff)
		}
		wantLive := map[string][]string{"billing": {"#2", "#6", "#11"}, "repo": {"#30"}}
		if diff := cmp.Diff(wantLive, liveByFeature(rail)); diff != "" {
			t.Errorf("BuildRail() live by feature mismatch (-want +got):\n%s", diff)
		}
		billing := rail.Features[0]
		if got, want := billing.WaitingLine(), "2 blocked, 1 ready"; got != want {
			t.Errorf("billing.WaitingLine() = %q, want %q", got, want)
		}
		if !billing.Open {
			t.Error("BuildRail(open=feature:billing) billing.Open = false, want true")
		}
		if diff := cmp.Diff([]string{"#5"}, refs(rail.Settled.Items)); diff != "" {
			t.Errorf("BuildRail().Settled mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("names the project on a card and marks each feature once", func(t *testing.T) {
		t.Parallel()
		acme := railRow("1", "Billing", plan.GlyphFailed)
		acme.Repo, acme.Project, acme.ProjectHue = "acme/web", "acme", 1
		loose := railRow("2", "Billing", plan.GlyphRunning)
		loose.Repo = "olly/job-scraper"
		second := railRow("3", "Billing", plan.GlyphRunning)
		second.Repo, second.Project, second.ProjectHue = "acme/api", "acme", 1
		other := railRow("4", "Billing", plan.GlyphRunning)
		other.Repo, other.Project, other.ProjectHue = "beta/web", "beta", 2

		rail := view.BuildRail([]view.Row{acme, loose, second, other}, testNow, view.RailParams{})

		card := rail.NeedsYou[0]
		if got, want := card.Feature+" "+card.Ref, "Billing #1"; got != want {
			t.Errorf("BuildRail() card feature and ref = %q, want %q", got, want)
		}
		if got, want := card.Project, (view.ProjectMark{Name: "acme", Hue: 1}); got != want {
			t.Errorf("BuildRail() card.Project = %v, want %v", got, want)
		}
		wantMarks := []view.ProjectMark{{Name: "acme", Hue: 1}, {Name: "beta", Hue: 2}}
		if diff := cmp.Diff(wantMarks, rail.Features[0].Marks); diff != "" {
			t.Errorf("BuildRail() Billing marks mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("a loose repo card has no mark", func(t *testing.T) {
		t.Parallel()
		row := railRow("5", "", plan.GlyphFailed)
		row.Repo = "olly/job-scraper"

		card := view.BuildRail([]view.Row{row}, testNow, view.RailParams{}).NeedsYou[0]

		if card.Feature != "olly/job-scraper" || card.Ref != "#5" || card.Project != (view.ProjectMark{}) {
			t.Errorf("BuildRail(loose) card = {%q %q %v}, want {olly/job-scraper #5 no mark}", card.Feature, card.Ref, card.Project)
		}
	})

	t.Run("awaiting input", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name      string
			verbs     []plan.Verb
			wantReply bool
			wantVerb  plan.Verb
		}{
			{name: "a reply replaces the verb", verbs: []plan.Verb{plan.VerbFollowUp, plan.VerbReRun}, wantReply: true},
			{name: "no follow-up keeps the verb", verbs: []plan.Verb{plan.VerbReRun}, wantVerb: plan.VerbReRun},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				row := railRow("3", "billing", plan.GlyphAttention)
				row.State = plan.AwaitingInput.String()
				row.Detail = "Which database?"
				row.Verbs = tt.verbs

				card := view.BuildRail([]view.Row{row}, testNow, view.RailParams{}).NeedsYou[0]

				if card.Reply != tt.wantReply || card.Verb != tt.wantVerb || card.Detail != row.Detail {
					t.Errorf("BuildRail(%v) card = {Reply: %v, Verb: %q, Detail: %q}, want {%v %q %q}",
						tt.verbs, card.Reply, card.Verb, card.Detail, tt.wantReply, tt.wantVerb, row.Detail)
				}
			})
		}
	})

	t.Run("scope", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name         string
			params       view.RailParams
			wantNeedsYou []string
			wantLive     map[string][]string
		}{
			{
				name:         "a project keeps admitted chains",
				params:       acmeScope,
				wantNeedsYou: []string{"#1", "#5"},
				wantLive:     map[string][]string{"checkout": {"#2"}},
			},
			{
				name:         "a repo",
				params:       view.RailParams{Repo: "other/repo"},
				wantNeedsYou: []string{"#1", "#3", "#5"},
				wantLive:     map[string][]string{"billing": {"#4"}},
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				rail := view.BuildRail(scopedRailRows(), testNow, tt.params)

				if diff := cmp.Diff(tt.wantNeedsYou, refs(rail.NeedsYou)); diff != "" {
					t.Errorf("BuildRail(%+v).NeedsYou mismatch (-want +got):\n%s", tt.params, diff)
				}
				if diff := cmp.Diff(tt.wantLive, liveByFeature(rail)); diff != "" {
					t.Errorf("BuildRail(%+v) live by feature mismatch (-want +got):\n%s", tt.params, diff)
				}
			})
		}
	})

	t.Run("links carry the project", func(t *testing.T) {
		t.Parallel()
		rail := view.BuildRail(scopedRailRows(), testNow, acmeScope)

		if got, want := rail.NeedsYou[0].Path, "/s/acme/web/1?project=acme"; got != want {
			t.Errorf("BuildRail(project=acme) card.Path = %q, want %q", got, want)
		}
	})

	t.Run("a query narrows each section", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name     string
			query    string
			needsYou []string
			live     map[string][]string
			settled  []string
		}{
			{
				name:     "all",
				needsYou: []string{"#12"},
				live:     map[string][]string{"billing": {"#2", "#3"}, "checkout": {"#40"}},
				settled:  []string{"#5"},
			},
			{name: "issue reference", query: "#12", needsYou: []string{"#12"}},
			{name: "issue number", query: "12", needsYou: []string{"#12"}},
			{name: "case insensitive", query: "INVOICE", needsYou: []string{"#12"}},
			{name: "repo", query: "web", live: map[string][]string{"checkout": {"#40"}}},
			{name: "feature", query: "billing", needsYou: []string{"#12"}, live: map[string][]string{"billing": {"#2", "#3"}}},
			{name: "title", query: "refund", live: map[string][]string{"billing": {"#3"}}},
			{name: "multiple terms", query: "billing refund", live: map[string][]string{"billing": {"#3"}}},
			{name: "no match", query: "refund cart"},
			{name: "settled", query: "old", settled: []string{"#5"}},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				rail := view.BuildRail(searchRows(), testNow, view.RailParams{Query: tt.query})

				if diff := cmp.Diff(tt.needsYou, refs(rail.NeedsYou)); diff != "" {
					t.Errorf("BuildRail(%q).NeedsYou mismatch (-want +got):\n%s", tt.query, diff)
				}
				if diff := cmp.Diff(tt.live, liveByFeature(rail), cmpopts.EquateEmpty()); diff != "" {
					t.Errorf("BuildRail(%q) live by feature mismatch (-want +got):\n%s", tt.query, diff)
				}
				if diff := cmp.Diff(tt.settled, refs(rail.Settled.Items)); diff != "" {
					t.Errorf("BuildRail(%q).Settled mismatch (-want +got):\n%s", tt.query, diff)
				}
			})
		}
	})

	t.Run("survivors are forced open only under a query", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			query    string
			wantOpen bool
		}{
			{query: "", wantOpen: false},
			{query: "billing", wantOpen: true},
		}
		for _, tt := range tests {
			t.Run(tt.query, func(t *testing.T) {
				t.Parallel()
				rail := view.BuildRail(searchRows(), testNow, view.RailParams{Query: tt.query})

				if rail.Query != tt.query || rail.Features[0].Open != tt.wantOpen || rail.Settled.Open != tt.wantOpen {
					t.Errorf("BuildRail(%q) = {Query: %q, feature open: %v, settled open: %v}, want {%q %v %v}",
						tt.query, rail.Query, rail.Features[0].Open, rail.Settled.Open, tt.query, tt.wantOpen, tt.wantOpen)
				}
			})
		}
	})
}

func TestRail_NoMatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		query string
		want  bool
	}{
		{query: "zzz", want: true},
		{query: "", want: false},
		{query: "billing", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			t.Parallel()
			if got := view.BuildRail(searchRows(), testNow, view.RailParams{Query: tt.query}).NoMatch(); got != tt.want {
				t.Errorf("BuildRail(%q).NoMatch() = %v, want %v", tt.query, got, tt.want)
			}
		})
	}
}

func TestRailScope(t *testing.T) {
	t.Parallel()

	project, repo := view.RailScope("http://localhost/tickets?project=acme&repo=acme/web")
	if project != "acme" || repo != "acme/web" {
		t.Errorf("RailScope(?project=acme&repo=acme/web) = %q, %q, want acme, acme/web", project, repo)
	}
}
