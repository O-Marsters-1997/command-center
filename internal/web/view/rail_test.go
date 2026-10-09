package view

import (
	"slices"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func railRow(n, feature, glyph string) Row {
	return Row{URL: "sandbox://" + n, Repo: "repo", Feature: feature, Glyph: glyph, Verbs: []string{"re-run"}}
}

func refs(items []RailItem) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.Ref)
	}
	return out
}

func TestBuildRailGroupsByAttention(t *testing.T) {
	t.Parallel()

	rows := []Row{
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

	rail := buildRail(rows, time.Now(), RailParams{Open: []string{"feature:billing"}})

	if got := refs(rail.NeedsYou); !slices.Equal(got, []string{"#1", "#12"}) {
		t.Errorf("NeedsYou = %v, want %v", got, []string{"#1", "#12"})
	}
	if len(rail.Features) != 2 {
		t.Fatalf("Features = %d, want billing and the repo group only (idle has no live work)", len(rail.Features))
	}
	billing := rail.Features[0]
	if got := refs(billing.Live); !slices.Equal(got, []string{"#2", "#6", "#11"}) {
		t.Errorf("billing live = %v, want %v", got, []string{"#2", "#6", "#11"})
	}
	if got := billing.WaitingLine(); got != "2 blocked, 1 ready" {
		t.Errorf("WaitingLine() = %q, want %q", got, "2 blocked, 1 ready")
	}
	if !billing.Open {
		t.Error("billing is in the open cookie but rendered closed")
	}
	if rail.Features[1].Name != "repo" {
		t.Errorf("a ticket with no feature groups under %q, got %q", "repo", rail.Features[1].Name)
	}
	if got := refs(rail.Settled.Items); !slices.Equal(got, []string{"#5"}) {
		t.Errorf("Settled = %v, want %v", got, []string{"#5"})
	}
}
