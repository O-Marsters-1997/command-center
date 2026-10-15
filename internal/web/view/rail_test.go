package view

import (
	"slices"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func railRow(n, feature string, glyph plan.GlyphWord) Row {
	return Row{URL: "sandbox://" + n, Repo: "repo", Feature: feature, Glyph: glyph, Verbs: []plan.Verb{plan.VerbReRun}}
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

	rail := buildRail(rows, testNow, RailParams{Open: []string{"feature:billing"}})

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

func TestBuildRailNamesTheProjectOnACardAndMarksEachFeatureOnce(t *testing.T) {
	t.Parallel()

	acme := railRow("1", "Billing", plan.GlyphFailed)
	acme.Repo, acme.Project, acme.ProjectHue = "acme/web", "acme", 1
	loose := railRow("2", "Billing", plan.GlyphRunning)
	loose.Repo = "olly/job-scraper"
	second := railRow("3", "Billing", plan.GlyphRunning)
	second.Repo, second.Project, second.ProjectHue = "acme/api", "acme", 1
	other := railRow("4", "Billing", plan.GlyphRunning)
	other.Repo, other.Project, other.ProjectHue = "beta/web", "beta", 2

	rail := buildRail([]Row{acme, loose, second, other}, testNow, RailParams{})

	card := rail.NeedsYou[0]
	if card.Context != "acme · Billing #1" || card.Project != (ProjectMark{Name: "acme", Hue: 1}) {
		t.Errorf("card = {%q %v}, want acme · Billing #1 with the hue 1 mark", card.Context, card.Project)
	}
	want := []ProjectMark{{Name: "acme", Hue: 1}, {Name: "beta", Hue: 2}}
	if got := rail.Features[0].Marks; !slices.Equal(got, want) {
		t.Errorf("Billing marks = %v, want %v", got, want)
	}
}

func TestBuildRailShowsALooseRepoWithoutAMark(t *testing.T) {
	t.Parallel()

	row := railRow("5", "", plan.GlyphFailed)
	row.Repo = "olly/job-scraper"

	rail := buildRail([]Row{row}, testNow, RailParams{})

	if got := rail.NeedsYou[0]; got.Context != "olly/job-scraper #5" || got.Project.Name != "" {
		t.Errorf("loose card = {%q %v}, want olly/job-scraper #5 and no mark", got.Context, got.Project)
	}
}

func TestBuildRailGivesAnAwaitingInputCardReplyInPlaceOfAVerb(t *testing.T) {
	t.Parallel()

	row := railRow("3", "billing", plan.GlyphAttention)
	row.State = plan.AwaitingInput.String()
	row.Reason = "Which database?"
	row.Verbs = []plan.Verb{plan.VerbFollowUp, plan.VerbReRun}

	rail := buildRail([]Row{row}, testNow, RailParams{})

	if len(rail.NeedsYou) != 1 {
		t.Fatalf("NeedsYou = %d cards, want 1", len(rail.NeedsYou))
	}
	got := rail.NeedsYou[0]
	if !got.Reply || got.Verb != "" || got.Reason != "Which database?" {
		t.Errorf("card = {Reply: %v, Verb: %q, Reason: %q}, want a Reply card with no verb carrying the question",
			got.Reply, got.Verb, got.Reason)
	}
}

func TestBuildRailKeepsTheVerbWhenAnAwaitingInputRunCannotBeRepliedTo(t *testing.T) {
	t.Parallel()

	row := railRow("4", "billing", plan.GlyphAttention)
	row.State = plan.AwaitingInput.String()
	row.Reason = "Which database?"
	row.Verbs = []plan.Verb{plan.VerbReRun}

	got := buildRail([]Row{row}, testNow, RailParams{}).NeedsYou[0]

	if got.Reply || got.Verb != plan.VerbReRun {
		t.Errorf("card = {Reply: %v, Verb: %q}, want no Reply and %q", got.Reply, got.Verb, plan.VerbReRun)
	}
}
