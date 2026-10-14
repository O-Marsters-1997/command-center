package view

import (
	"slices"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func scopedRailRows() []Row {
	return []Row{
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

func TestBuildRailNarrowsToAProjectAndKeepsAdmittedChains(t *testing.T) {
	t.Parallel()

	params := RailParams{Project: "acme", projectRepos: []string{"acme/web", "acme/api"}}
	rail := buildRail(scopedRailRows(), testNow, params)

	if got := refs(rail.NeedsYou); !slices.Equal(got, []string{"#1", "#5"}) {
		t.Errorf("NeedsYou = %v, want acme's #1 and the out-of-project member #5 of its chain", got)
	}
	if len(rail.Features) != 1 || rail.Features[0].Name != "checkout" {
		t.Errorf("Features = %+v, want only checkout", rail.Features)
	}
}

func TestBuildRailNarrowsToARepo(t *testing.T) {
	t.Parallel()

	rail := buildRail(scopedRailRows(), testNow, RailParams{Repo: "other/repo"})

	if got := refs(rail.NeedsYou); !slices.Equal(got, []string{"#1", "#3", "#5"}) {
		t.Errorf("NeedsYou = %v, want %v (#1 rides in on #5's chain)", got, []string{"#1", "#3", "#5"})
	}
	if len(rail.Features) != 1 || rail.Features[0].Name != "billing" {
		t.Errorf("Features = %+v, want only billing", rail.Features)
	}
}

func TestBuildRailLinksCarryTheProject(t *testing.T) {
	t.Parallel()

	params := RailParams{Project: "acme", projectRepos: []string{"acme/web", "acme/api"}}
	rail := buildRail(scopedRailRows(), testNow, params)

	if want := "/s/acme/web/1?project=acme"; rail.NeedsYou[0].Path != want {
		t.Errorf("Path = %q, want %q", rail.NeedsYou[0].Path, want)
	}
}

func TestRailScopeReadsTheCurrentURLQuery(t *testing.T) {
	t.Parallel()

	project, repo := RailScope("http://localhost/tickets?project=acme&repo=acme/web")
	if project != "acme" || repo != "acme/web" {
		t.Errorf("RailScope() = %q, %q, want acme, acme/web", project, repo)
	}
}
