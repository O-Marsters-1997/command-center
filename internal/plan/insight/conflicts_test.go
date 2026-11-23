package insight_test

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan/insight"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
)

func day(d int) time.Time { return time.Date(2026, 6, d, 12, 0, 0, 0, time.UTC) }

func conflict(ticket, feature string, d int, paths ...insight.ConflictPath) insight.ConflictFact {
	return insight.ConflictFact{Ticket: ticket, Feature: feature, At: day(d), Paths: paths}
}

func hand(path string) insight.ConflictPath { return insight.ConflictPath{Path: path} }

func gen(path string) insight.ConflictPath { return insight.ConflictPath{Path: path, Generated: true} }

func TestHotspots(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		conflicts []insight.ConflictFact
		want      []insight.HotspotRow
	}{
		{name: "no conflicts", want: []insight.HotspotRow{}},
		{
			name: "a ticket conflicting again on a path is a repeat, not a second ticket",
			conflicts: []insight.ConflictFact{
				conflict("a", "f", 3, hand("x.go")),
				conflict("a", "f", 1, hand("x.go"), hand("y.go")),
				conflict("a", "f", 2, hand("x.go")),
			},
			want: []insight.HotspotRow{
				{Path: "x.go", Tickets: 1, Repeats: 2},
				{Path: "y.go", Tickets: 1},
			},
		},
		{
			name: "a new path on a returning ticket is not a repeat",
			conflicts: []insight.ConflictFact{
				conflict("a", "f", 1, hand("x.go")),
				conflict("a", "f", 2, hand("y.go")),
			},
			want: []insight.HotspotRow{{Path: "x.go", Tickets: 1}, {Path: "y.go", Tickets: 1}},
		},
		{
			name: "paths rank by tickets then repeats, and keep the generated tag",
			conflicts: []insight.ConflictFact{
				conflict("a", "f", 1, gen("gen.sql"), hand("b.go"), hand("c.go")),
				conflict("b", "f", 2, gen("gen.sql"), hand("b.go")),
				conflict("b", "f", 3, hand("b.go")),
			},
			want: []insight.HotspotRow{
				{Path: "b.go", Tickets: 2, Repeats: 1},
				{Path: "gen.sql", Generated: true, Tickets: 2},
				{Path: "c.go", Tickets: 1},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if diff := cmp.Diff(tt.want, insight.Hotspots(tt.conflicts)); diff != "" {
				t.Errorf("Hotspots() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStackPairs(t *testing.T) {
	t.Parallel()
	span := func(ticket, feature string, from int, to option.Option[time.Time]) insight.OpenSpan {
		return insight.OpenSpan{Ticket: ticket, Feature: feature, From: day(from), To: to}
	}
	closed := func(d int) option.Option[time.Time] { return option.Some(day(d)) }
	tests := []struct {
		name  string
		facts insight.ConflictFacts
		want  []insight.StackPair
	}{
		{name: "nothing recorded"},
		{
			name: "overlapping spans on a shared hand-written path pair",
			facts: insight.ConflictFacts{
				Conflicts: []insight.ConflictFact{
					conflict("b", "f", 3, hand("x.go")),
					conflict("a", "f", 4, hand("x.go"), hand("y.go")),
					conflict("a", "f", 5, hand("x.go")),
				},
				Spans: []insight.OpenSpan{span("a", "f", 1, closed(6)), span("b", "f", 2, option.None[time.Time]())},
			},
			want: []insight.StackPair{{A: "a", B: "b", Path: "x.go"}},
		},
		{
			name: "one pair per shared path",
			facts: insight.ConflictFacts{
				Conflicts: []insight.ConflictFact{
					conflict("a", "f", 3, hand("y.go"), hand("x.go")),
					conflict("b", "f", 3, hand("x.go"), hand("y.go")),
				},
				Spans: []insight.OpenSpan{span("a", "f", 1, closed(9)), span("b", "f", 2, closed(9))},
			},
			want: []insight.StackPair{{A: "a", B: "b", Path: "x.go"}, {A: "a", B: "b", Path: "y.go"}},
		},
		{
			name: "spans that do not overlap do not pair",
			facts: insight.ConflictFacts{
				Conflicts: []insight.ConflictFact{conflict("a", "f", 2, hand("x.go")), conflict("b", "f", 6, hand("x.go"))},
				Spans:     []insight.OpenSpan{span("a", "f", 1, closed(4)), span("b", "f", 4, closed(8))},
			},
		},
		{
			name: "tickets in different features do not pair",
			facts: insight.ConflictFacts{
				Conflicts: []insight.ConflictFact{conflict("a", "f", 2, hand("x.go")), conflict("b", "g", 2, hand("x.go"))},
				Spans:     []insight.OpenSpan{span("a", "f", 1, closed(9)), span("b", "g", 1, closed(9))},
			},
		},
		{
			name: "generated paths never pair",
			facts: insight.ConflictFacts{
				Conflicts: []insight.ConflictFact{conflict("a", "f", 2, gen("x.sql")), conflict("b", "f", 2, gen("x.sql"))},
				Spans:     []insight.OpenSpan{span("a", "f", 1, closed(9)), span("b", "f", 1, closed(9))},
			},
		},
		{
			name: "a ticket with no span does not pair",
			facts: insight.ConflictFacts{
				Conflicts: []insight.ConflictFact{conflict("a", "f", 2, hand("x.go")), conflict("b", "f", 2, hand("x.go"))},
				Spans:     []insight.OpenSpan{span("a", "f", 1, closed(9))},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if diff := cmp.Diff(tt.want, insight.StackPairs(tt.facts)); diff != "" {
				t.Errorf("StackPairs() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
