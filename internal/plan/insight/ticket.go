package insight

import (
	"cmp"
	"slices"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	"github.com/O-Marsters-1997/command-center/internal/plan/spend"
)

type PushVerdict string

const (
	PushPending PushVerdict = "pending"
	PushGreen   PushVerdict = "green"
	PushRed     PushVerdict = "red"
)

type PushFact struct {
	At      time.Time
	Verdict PushVerdict
}

type RunFact struct {
	ID                 int64
	Step               string
	Kind               string
	StartedAt, EndedAt time.Time
	WeightUSD          float64
	Tokens             int64
	Tools              map[string]int
	Outcome            string
	Phases             []SkillLoad
}

type TicketFacts struct {
	Runs     []RunFact
	Pushes   []PushFact
	MergedAt option.Option[time.Time]
}

type SpendStep struct {
	At     time.Time
	Tokens int64
}

type ToolCount struct {
	Name  string
	Calls int
}

type TicketReport struct {
	Merged        bool
	LaunchToMerge option.Option[time.Duration]
	AgentsWorking time.Duration
	Tokens        int64
	PctWeek       option.Option[float64]
	ToolCalls     int
	PushesToGreen option.Option[int]
	Cumulative    []SpendStep
	Tools         []ToolCount
	Runs          []RunFact
	Stages        []StageShare
}

func DeriveTicket(f TicketFacts, fit spend.Result) TicketReport {
	r := TicketReport{Merged: f.MergedAt.IsSome(), Runs: f.Runs}
	var weight float64
	var first time.Time
	tools := make(map[string]int)
	for _, run := range f.Runs {
		weight += run.WeightUSD
		r.Tokens += run.Tokens
		for name, n := range run.Tools {
			tools[name] += n
			r.ToolCalls += n
		}
		if run.StartedAt.IsZero() {
			continue
		}
		r.AgentsWorking += run.EndedAt.Sub(run.StartedAt)
		if first.IsZero() || run.StartedAt.Before(first) {
			first = run.StartedAt
		}
	}
	if r.Merged && !first.IsZero() {
		r.LaunchToMerge = option.Some(f.MergedAt.Unwrap().Sub(first))
	}
	if fit.Samples >= spend.MinSamples {
		r.PctWeek = option.Some(spend.PctWeek(weight, fit.Factor))
	}
	if i := slices.IndexFunc(f.Pushes, func(p PushFact) bool { return p.Verdict == PushGreen }); i >= 0 {
		r.PushesToGreen = option.Some(i + 1)
	}

	ended := slices.Clone(f.Runs)
	slices.SortStableFunc(ended, func(a, b RunFact) int { return a.EndedAt.Compare(b.EndedAt) })
	var total int64
	for _, run := range ended {
		total += run.Tokens
		r.Cumulative = append(r.Cumulative, SpendStep{At: run.EndedAt, Tokens: total})
	}

	loads, followUp, resolve, recorded := StageFacts(f.Runs)
	r.Stages = ticketStages(TicketFact{
		Tokens: r.Tokens, Recorded: recorded, Loads: loads, FollowUpTokens: followUp, ResolveTokens: resolve,
	})

	for name, n := range tools {
		r.Tools = append(r.Tools, ToolCount{Name: name, Calls: n})
	}
	slices.SortFunc(r.Tools, func(a, b ToolCount) int {
		return cmp.Or(cmp.Compare(b.Calls, a.Calls), cmp.Compare(a.Name, b.Name))
	})
	return r
}
