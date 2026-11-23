package insight_test

import (
	"fmt"
	"math"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan/insight"
)

func skill(name string) insight.Stage { return insight.Stage{Kind: insight.StageSkill, Skill: name} }

func kind(k insight.StageKind) insight.Stage { return insight.Stage{Kind: k} }

func recorded(repo string, tokens int64, loads ...insight.SkillLoad) insight.TicketFact {
	return insight.TicketFact{Ticket: repo, Repo: repo, Feature: "f-" + repo, Recorded: true, Tokens: tokens, Loads: loads}
}

func load(name string, order int, tokens int64) insight.SkillLoad {
	return insight.SkillLoad{Skill: name, Order: order, Tokens: tokens}
}

func TestDeriveStages(t *testing.T) {
	t.Parallel()

	t.Run("configured stages keep their order and fold the rest into other skills", func(t *testing.T) {
		t.Parallel()
		f := recorded("a", 250, load("tdd", 0, 100), load("zzz", 1, 50))
		f.FollowUpTokens, f.ResolveTokens = 30, 20
		got := insight.DeriveStages([]insight.TicketFact{f}, []string{"grill", "tdd"}, false)

		wantOrder := []insight.Stage{
			kind(insight.StageSetup), skill("grill"), skill("tdd"),
			kind(insight.StageOtherSkills), kind(insight.StageFollowUps), kind(insight.StageResolve),
		}
		if diff := cmp.Diff(wantOrder, got.Order); diff != "" {
			t.Errorf("DeriveStages().Order mismatch (-want +got):\n%s", diff)
		}
		wantShare := []insight.StageShare{
			{Stage: wantOrder[0], Tokens: 50, Share: 0.2},
			{Stage: wantOrder[1]},
			{Stage: wantOrder[2], Tokens: 100, Share: 0.4},
			{Stage: wantOrder[3], Tokens: 50, Share: 0.2},
			{Stage: wantOrder[4], Tokens: 30, Share: 0.12},
			{Stage: wantOrder[5], Tokens: 20, Share: 0.08},
		}
		if diff := cmp.Diff(wantShare, got.Share); diff != "" {
			t.Errorf("DeriveStages().Share mismatch (-want +got):\n%s", diff)
		}
		if got.Source != insight.StagesFromConfig {
			t.Errorf("DeriveStages().Source = %q, want %q", got.Source, insight.StagesFromConfig)
		}
	})

	t.Run("observed stages order by median load position", func(t *testing.T) {
		t.Parallel()
		tickets := []insight.TicketFact{
			recorded("a", 30, load("late", 1, 10), load("early", 0, 10)),
			recorded("b", 30, load("early", 0, 10), load("late", 1, 10)),
			recorded("c", 30, load("late", 1, 10), load("early", 0, 10)),
		}
		got := insight.DeriveStages(tickets, nil, false)
		want := []insight.Stage{kind(insight.StageSetup), skill("early"), skill("late")}
		if diff := cmp.Diff(want, got.Order); diff != "" {
			t.Errorf("DeriveStages().Order mismatch (-want +got):\n%s", diff)
		}
		if got.Source != insight.StagesFromObserved {
			t.Errorf("DeriveStages().Source = %q, want %q", got.Source, insight.StagesFromObserved)
		}
	})

	t.Run("observed stages stop at the cap", func(t *testing.T) {
		t.Parallel()
		var loads []insight.SkillLoad
		for i := range insight.MaxObservedStages + 2 {
			loads = append(loads, load(fmt.Sprintf("s%d", i), i, 10))
		}
		got := insight.DeriveStages([]insight.TicketFact{recorded("a", 80, loads...)}, nil, false)

		skills := 0
		for _, s := range got.Order {
			if s.Kind == insight.StageSkill {
				skills++
			}
		}
		if skills != insight.MaxObservedStages {
			t.Errorf("DeriveStages() skill stages = %d, want %d", skills, insight.MaxObservedStages)
		}
		var other int64
		for _, s := range got.Share {
			if s.Stage.Kind == insight.StageOtherSkills {
				other = s.Tokens
			}
		}
		if other != 20 {
			t.Errorf("DeriveStages() other skills tokens = %d, want 20", other)
		}
		if len(got.Skills) != insight.MaxObservedStages+2 {
			t.Errorf("DeriveStages().Skills = %d rows, want every loaded skill", len(got.Skills))
		}
	})

	t.Run("unrecorded tickets are counted and left out", func(t *testing.T) {
		t.Parallel()
		missing := recorded("b", 999, load("tdd", 0, 999))
		missing.Recorded = false
		got := insight.DeriveStages([]insight.TicketFact{recorded("a", 100, load("tdd", 0, 60)), missing}, nil, false)

		if got.Unrecorded != 1 {
			t.Errorf("DeriveStages().Unrecorded = %d, want 1", got.Unrecorded)
		}
		wantSkills := []insight.SkillRow{{Skill: "tdd", Tickets: 1, Share: 1, Tokens: 60}}
		if diff := cmp.Diff(wantSkills, got.Skills); diff != "" {
			t.Errorf("DeriveStages().Skills mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("skills share is tickets that loaded it over recorded tickets", func(t *testing.T) {
		t.Parallel()
		tickets := []insight.TicketFact{
			recorded("a", 10, load("tdd", 0, 5), load("review", 1, 5)),
			recorded("b", 10, load("tdd", 0, 10)),
			recorded("c", 10),
			recorded("d", 10),
		}
		got := insight.DeriveStages(tickets, nil, false)
		want := []insight.SkillRow{
			{Skill: "tdd", Tickets: 2, Share: 0.5, Tokens: 15},
			{Skill: "review", Tickets: 1, Share: 0.25, Tokens: 5},
		}
		if diff := cmp.Diff(want, got.Skills); diff != "" {
			t.Errorf("DeriveStages().Skills mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("bars sum to one per repo or per feature", func(t *testing.T) {
		t.Parallel()
		tickets := []insight.TicketFact{
			recorded("x", 100, load("tdd", 0, 40)),
			recorded("x", 100, load("tdd", 0, 70)),
			recorded("y", 90, load("tdd", 0, 10)),
		}
		tickets[1].Feature = "f-y"
		for _, byFeature := range []bool{false, true} {
			got := insight.DeriveStages(tickets, nil, byFeature)
			if len(got.Bars) != 2 {
				t.Fatalf("DeriveStages(byFeature=%v).Bars = %d, want 2", byFeature, len(got.Bars))
			}
			for _, bar := range got.Bars {
				var sum float64
				for _, s := range bar.Shares {
					sum += s.Share
				}
				if math.Abs(sum-1) > 1e-9 {
					t.Errorf("DeriveStages(byFeature=%v) bar %q shares sum to %v, want 1", byFeature, bar.Label, sum)
				}
			}
		}
		byRepo := insight.DeriveStages(tickets, nil, false)
		if labels := []string{byRepo.Bars[0].Label, byRepo.Bars[1].Label}; !slices.Equal(labels, []string{"x", "y"}) {
			t.Errorf("DeriveStages(byFeature=false) labels = %q, want repos x, y", labels)
		}
	})

	t.Run("no tickets gives no stages", func(t *testing.T) {
		t.Parallel()
		got := insight.DeriveStages(nil, nil, false)
		if len(got.Bars) != 0 || len(got.Skills) != 0 || got.Unrecorded != 0 {
			t.Errorf("DeriveStages(nil) = %+v, want empty", got)
		}
	})
}

func TestStageFacts(t *testing.T) {
	t.Parallel()
	phased := func(runKind string, tokens int64, phases ...insight.SkillLoad) insight.RunFact {
		return insight.RunFact{Kind: runKind, Tokens: tokens, Phases: phases}
	}

	t.Run("splits agent loads from follow-up and resolve runs", func(t *testing.T) {
		t.Parallel()
		runs := []insight.RunFact{
			phased("agent", 100, load("", 0, 10), load("tdd", 1, 90)),
			phased("agent", 50, load("tdd", 0, 20), load("review", 1, 30)),
			phased("follow_up", 7, load("", 0, 7)),
			phased("resolve", 3, load("", 0, 3)),
		}
		loads, followUp, resolve, ok := insight.StageFacts(runs)
		want := []insight.SkillLoad{load("tdd", 0, 110), load("review", 1, 30)}
		if diff := cmp.Diff(want, loads); diff != "" {
			t.Errorf("StageFacts() loads mismatch (-want +got):\n%s", diff)
		}
		if !ok || followUp != 7 || resolve != 3 {
			t.Errorf("StageFacts() = recorded %v, follow-up %d, resolve %d, want true, 7, 3", ok, followUp, resolve)
		}
	})

	t.Run("a run without phases leaves the ticket unrecorded", func(t *testing.T) {
		t.Parallel()
		runs := []insight.RunFact{phased("agent", 100, load("tdd", 0, 100)), phased("follow_up", 5)}
		if _, _, _, ok := insight.StageFacts(runs); ok {
			t.Error("StageFacts() recorded = true, want false")
		}
	})

	t.Run("no counted runs is unrecorded", func(t *testing.T) {
		t.Parallel()
		if _, _, _, ok := insight.StageFacts([]insight.RunFact{phased("command", 0)}); ok {
			t.Error("StageFacts() recorded = true, want false")
		}
	})
}
