package insight

import (
	"cmp"
	"maps"
	"slices"
)

type StageKind string

const (
	StageSetup       StageKind = "setup"
	StageSkill       StageKind = "skill"
	StageOtherSkills StageKind = "other_skills"
	StageFollowUps   StageKind = "follow_ups"
	StageResolve     StageKind = "resolve"
)

type Stage struct {
	Kind  StageKind
	Skill string
}

type StageSource string

const (
	StagesFromConfig   StageSource = "config"
	StagesFromObserved StageSource = "observed"
)

const MaxObservedStages = 6

type SkillLoad struct {
	Skill  string
	Order  int
	Tokens int64
}

type StageShare struct {
	Stage  Stage
	Tokens int64
	Share  float64
}

type StageBar struct {
	Label  string
	Shares []StageShare
}

type SkillRow struct {
	Skill   string
	Tickets int
	Share   float64
	Tokens  int64
}

type Stages struct {
	Order      []Stage
	Source     StageSource
	Share      []StageShare
	Bars       []StageBar
	Skills     []SkillRow
	Unrecorded int
}

func StageFacts(runs []RunFact) (loads []SkillLoad, followUp, resolve int64, recorded bool) {
	counted, missing := false, false
	for _, run := range runs {
		switch run.Kind {
		case "agent":
			for _, p := range run.Phases {
				if p.Skill == "" {
					continue
				}
				if i := slices.IndexFunc(loads, func(l SkillLoad) bool { return l.Skill == p.Skill }); i >= 0 {
					loads[i].Tokens += p.Tokens
					continue
				}
				loads = append(loads, SkillLoad{Skill: p.Skill, Order: len(loads), Tokens: p.Tokens})
			}
		case "follow_up":
			followUp += run.Tokens
		case "resolve":
			resolve += run.Tokens
		default:
			continue
		}
		counted = true
		missing = missing || len(run.Phases) == 0
	}
	return loads, followUp, resolve, counted && !missing
}

func ticketTokens(f TicketFact, place func(skill string) Stage) map[Stage]int64 {
	tokens := make(map[Stage]int64)
	setup := f.Tokens - f.FollowUpTokens - f.ResolveTokens
	for _, l := range f.Loads {
		tokens[place(l.Skill)] += l.Tokens
		setup -= l.Tokens
	}
	tokens[Stage{Kind: StageSetup}] = max(setup, 0)
	tokens[Stage{Kind: StageFollowUps}] = f.FollowUpTokens
	tokens[Stage{Kind: StageResolve}] = f.ResolveTokens
	return tokens
}

func stageShares(order []Stage, tokens map[Stage]int64) []StageShare {
	var total int64
	for _, s := range order {
		total += tokens[s]
	}
	out := make([]StageShare, len(order))
	for i, s := range order {
		out[i] = StageShare{Stage: s, Tokens: tokens[s]}
		if total > 0 {
			out[i].Share = float64(tokens[s]) / float64(total)
		}
	}
	return out
}

func ticketStages(f TicketFact) []StageShare {
	if !f.Recorded {
		return nil
	}
	loads := slices.SortedFunc(slices.Values(f.Loads), func(a, b SkillLoad) int { return cmp.Compare(a.Order, b.Order) })
	own := func(skill string) Stage { return Stage{Kind: StageSkill, Skill: skill} }
	tokens := ticketTokens(f, own)
	order := []Stage{{Kind: StageSetup}}
	for _, l := range loads {
		order = append(order, own(l.Skill))
	}
	order = append(order, Stage{Kind: StageFollowUps}, Stage{Kind: StageResolve})
	order = slices.DeleteFunc(order, func(s Stage) bool { return tokens[s] == 0 })
	return stageShares(order, tokens)
}

func observedSkills(tickets []TicketFact) []string {
	positions := make(map[string][]float64)
	for _, f := range tickets {
		for _, l := range f.Loads {
			positions[l.Skill] = append(positions[l.Skill], float64(l.Order))
		}
	}
	skills := slices.SortedFunc(maps.Keys(positions), func(a, b string) int {
		return cmp.Or(cmp.Compare(median(positions[a]), median(positions[b])), cmp.Compare(a, b))
	})
	return skills[:min(len(skills), MaxObservedStages)]
}

func DeriveStages(tickets []TicketFact, configured []string, byFeature bool) Stages {
	var recorded []TicketFact
	out := Stages{Source: StagesFromConfig}
	for _, f := range tickets {
		if f.Recorded {
			recorded = append(recorded, f)
		} else {
			out.Unrecorded++
		}
	}

	skills := configured
	if len(skills) == 0 {
		skills, out.Source = observedSkills(recorded), StagesFromObserved
	}
	place := func(skill string) Stage {
		if slices.Contains(skills, skill) {
			return Stage{Kind: StageSkill, Skill: skill}
		}
		return Stage{Kind: StageOtherSkills}
	}

	totals := make(map[Stage]int64)
	groups := make(map[string]map[Stage]int64)
	loaded := make(map[string]*SkillRow)
	for _, f := range recorded {
		label := f.Repo
		if byFeature {
			label = f.Feature
		}
		if groups[label] == nil {
			groups[label] = make(map[Stage]int64)
		}
		for s, n := range ticketTokens(f, place) {
			totals[s] += n
			groups[label][s] += n
		}
		for _, l := range f.Loads {
			row := loaded[l.Skill]
			if row == nil {
				row = &SkillRow{Skill: l.Skill}
				loaded[l.Skill] = row
			}
			row.Tickets++
			row.Tokens += l.Tokens
		}
	}

	out.Order = []Stage{{Kind: StageSetup}}
	for _, s := range skills {
		out.Order = append(out.Order, Stage{Kind: StageSkill, Skill: s})
	}
	out.Order = append(out.Order, Stage{Kind: StageOtherSkills}, Stage{Kind: StageFollowUps}, Stage{Kind: StageResolve})
	out.Order = slices.DeleteFunc(out.Order, func(s Stage) bool { return s.Kind != StageSkill && totals[s] == 0 })

	out.Share = stageShares(out.Order, totals)
	for _, label := range slices.Sorted(maps.Keys(groups)) {
		out.Bars = append(out.Bars, StageBar{Label: label, Shares: stageShares(out.Order, groups[label])})
	}
	for row := range maps.Values(loaded) {
		row.Share = float64(row.Tickets) / float64(len(recorded))
		out.Skills = append(out.Skills, *row)
	}
	slices.SortFunc(out.Skills, func(a, b SkillRow) int {
		return cmp.Or(cmp.Compare(b.Tickets, a.Tickets), cmp.Compare(a.Skill, b.Skill))
	})
	return out
}
