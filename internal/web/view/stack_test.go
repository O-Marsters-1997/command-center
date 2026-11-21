package view_test

import (
	"path"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

func TestBuildStack(t *testing.T) {
	t.Parallel()
	const base = "https://github.com/acme/web/issues/"
	member := func(n string, glyph plan.GlyphWord, blockedBy ...string) view.Row {
		return view.Row{
			URL: base + n, Repo: "acme/web", Feature: "checkout", Title: "ticket " + n,
			Glyph: glyph, Label: string(glyph), BlockedBy: blockedBy,
		}
	}
	merged := member("1", plan.GlyphDone)
	merged.State = plan.PRMerged
	current := member("2", plan.GlyphRunning, base+"1")
	current.Alive, current.Elapsed = true, "4m"
	checking := member("3", plan.GlyphChecking, base+"2")
	ready := member("4", plan.GlyphReady, base+"1")
	blocked := member("5", plan.GlyphBlocked, base+"3")
	failed := member("6", plan.GlyphFailed)
	other := view.Row{URL: base + "9", Repo: "acme/web", Feature: "search", Glyph: plan.GlyphRunning}
	board := view.Board{Groups: []view.Group{{
		Root:     option.None[view.Row](),
		Children: []view.Row{merged, current, checking, ready, blocked, failed, other},
	}}}

	got := view.BuildStack(board, current)

	link := func(r view.Row, wave int, metric string) view.StackLink {
		return view.StackLink{
			Ref: "#" + path.Base(r.URL), Title: r.Title, Glyph: r.Glyph, Label: r.Label,
			Path: "/s/acme/web/" + path.Base(r.URL), Metric: metric, Wave: wave, Current: r.URL == current.URL,
		}
	}
	wantInFlight := []view.StackLink{link(current, 2, "4m"), link(checking, 3, "")}
	if diff := cmp.Diff(wantInFlight, got.InFlight); diff != "" {
		t.Errorf("BuildStack(#2).InFlight mismatch (-want +got):\n%s", diff)
	}
	wantFolds := []view.StackFold{
		{Label: "Ready to launch", Links: []view.StackLink{link(ready, 2, "")}},
		{Label: "Blocked", Links: []view.StackLink{link(blocked, 4, "")}},
		{Label: "Merged", Links: []view.StackLink{link(merged, 1, "")}},
	}
	if diff := cmp.Diff(wantFolds, got.Folds); diff != "" {
		t.Errorf("BuildStack(#2).Folds mismatch (-want +got):\n%s", diff)
	}
	if got.Merged != 1 || got.Total != 6 || got.MergedPercent != 16 {
		t.Errorf("BuildStack(#2) progress = %d/%d (%d%%), want 1/6 (16%%)", got.Merged, got.Total, got.MergedPercent)
	}
}
