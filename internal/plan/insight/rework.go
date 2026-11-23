package insight

import (
	"cmp"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan/option"
)

type ReworkMetric string

const (
	ReworkGreen      ReworkMetric = "green"
	ReworkConflicted ReworkMetric = "conflicted"
	ReworkPushes     ReworkMetric = "pushes"
)

type PushBand string

const (
	BandFirst     PushBand = "first"
	BandTwo       PushBand = "two"
	BandThreePlus PushBand = "three_plus"
)

type ReworkWeek struct {
	Start     time.Time
	Hit, Miss int
}

type BandColumn struct {
	Band         PushBand
	Count        int
	Share        float64
	MedianTokens int64
	MedianWall   time.Duration
}

type Rework struct {
	Tiles     []Tile
	Selected  ReworkMetric
	Weeks     []ReworkWeek
	Histogram []Bucket
	Columns   []BandColumn
}

var reworkMetrics = []ReworkMetric{ReworkGreen, ReworkConflicted, ReworkPushes}

var bands = []PushBand{BandFirst, BandTwo, BandThreePlus}

func DeriveRework(current, prior []TicketFact, r Range, metric ReworkMetric) Rework {
	sorted := slices.Clone(current)
	slices.SortStableFunc(sorted, func(a, b TicketFact) int { return a.MergedAt.Compare(b.MergedAt) })

	rework := Rework{Selected: metric}
	for _, m := range reworkMetrics {
		rate := reworkRate(m)
		cur := rate(sorted)
		tile := Tile{
			Metric: Metric(m), Median: cur, N: len(sorted),
			Change: change(cur, rate(prior), len(sorted), len(prior)),
		}
		if c, ok := tile.Change.Get(); ok && m == ReworkGreen {
			c.Tone = toneOf(-c.Fraction)
			tile.Change = option.Some(c)
		}
		buckets := bucketsOf(sorted, r, rate)
		for _, b := range buckets {
			tile.Spark = append(tile.Spark, b.Median)
		}
		rework.Tiles = append(rework.Tiles, tile)
		if m == metric {
			rework.Histogram = buckets
		}
	}
	rework.Weeks = reworkWeeks(sorted, reworkHit(metric))
	rework.Columns = bandColumns(sorted)
	return rework
}

func bandOf(f TicketFact) PushBand {
	n := len(f.Pushes)
	if i := slices.IndexFunc(f.Pushes, func(p PushFact) bool { return p.Verdict == PushGreen }); i >= 0 {
		n = i + 1
	}
	switch {
	case n <= 1:
		return BandFirst
	case n == 2:
		return BandTwo
	}
	return BandThreePlus
}

func reworkHit(m ReworkMetric) func(TicketFact) bool {
	return func(f TicketFact) bool {
		switch m {
		case ReworkGreen:
			return len(f.Pushes) == 1 && f.Pushes[0].Verdict == PushGreen
		case ReworkConflicted:
			return f.Conflicted == 0
		case ReworkPushes:
			return bandOf(f) != BandThreePlus
		}
		return false
	}
}

func reworkRate(m ReworkMetric) func([]TicketFact) float64 {
	hit := reworkHit(m)
	return func(facts []TicketFact) float64 {
		if m == ReworkConflicted {
			var refreshes, conflicted int
			for _, f := range facts {
				refreshes += f.Refreshes
				conflicted += f.Conflicted
			}
			if refreshes == 0 {
				return 0
			}
			return float64(conflicted) / float64(refreshes)
		}
		if len(facts) == 0 {
			return 0
		}
		var hits int
		for _, f := range facts {
			if hit(f) {
				hits++
			}
		}
		if m == ReworkGreen {
			return float64(hits) / float64(len(facts))
		}
		return float64(len(facts)-hits) / float64(len(facts))
	}
}

func reworkWeeks(sorted []TicketFact, hit func(TicketFact) bool) []ReworkWeek {
	if len(sorted) == 0 {
		return nil
	}
	first, last := bucketStart(sorted[0].MergedAt, 7), bucketStart(sorted[len(sorted)-1].MergedAt, 7)
	var weeks []ReworkWeek
	for start := first; !start.After(last); start = start.AddDate(0, 0, 7) {
		weeks = append(weeks, ReworkWeek{Start: start})
	}
	for _, f := range sorted {
		start := bucketStart(f.MergedAt, 7)
		w := &weeks[slices.IndexFunc(weeks, func(w ReworkWeek) bool { return w.Start.Equal(start) })]
		if hit(f) {
			w.Hit++
		} else {
			w.Miss++
		}
	}
	return weeks
}

func bandColumns(facts []TicketFact) []BandColumn {
	columns := make([]BandColumn, len(bands))
	for i, band := range bands {
		var tokens, wall []float64
		for _, f := range facts {
			if bandOf(f) == band {
				tokens = append(tokens, float64(f.Tokens))
				wall = append(wall, float64(f.MergedAt.Sub(f.FirstRunAt)))
			}
		}
		columns[i] = BandColumn{
			Band: band, Count: len(tokens),
			MedianTokens: int64(median(tokens)), MedianWall: time.Duration(median(wall)),
		}
		if len(facts) > 0 {
			columns[i].Share = float64(len(tokens)) / float64(len(facts))
		}
	}
	return columns
}

type ParkFact struct {
	Ticket, Repo, Reason, Detail string
	At                           time.Time
	Tokens                       int64
}

type FailedCheck struct {
	Ticket, Repo, Name string
	At                 time.Time
}

type FixRun struct {
	Ticket     string
	LaunchedAt time.Time
	Tokens     int64
	Checks     []string
}

type ReworkFacts struct {
	Parks  []ParkFact
	Failed []FailedCheck
	Fixes  []FixRun
}

type ParkRow struct {
	Reason, Example string
	Times           int
	Tokens          int64
}

type JobRow struct {
	Name, Repo string
	Failures   int
	FixTokens  int64
}

var variablePart = regexp.MustCompile(`[0-9/]`)

func ParkReason(detail string) string {
	head, _, _ := strings.Cut(detail, ";")
	words := strings.Fields(head)
	for i, w := range words {
		if variablePart.MatchString(w) {
			words[i] = "…"
		}
	}
	return strings.Join(words, " ")
}

func ParkLedger(parks []ParkFact) []ParkRow {
	index := map[string]int{}
	var rows []ParkRow
	for _, p := range parks {
		i, ok := index[p.Reason]
		if !ok {
			i = len(rows)
			index[p.Reason] = i
			rows = append(rows, ParkRow{Reason: p.Reason, Example: p.Detail})
		}
		rows[i].Times++
		rows[i].Tokens += p.Tokens
	}
	slices.SortFunc(rows, func(a, b ParkRow) int {
		return cmp.Or(cmp.Compare(b.Times, a.Times), cmp.Compare(b.Tokens, a.Tokens), cmp.Compare(a.Reason, b.Reason))
	})
	return rows
}

type jobKey struct{ name, repo string }

func JobLedger(f ReworkFacts, rollups map[string][]string) []JobRow {
	isRollup := func(repo, name string) bool {
		return slices.ContainsFunc(rollups[repo], func(g string) bool {
			matched, err := path.Match(g, name)
			return err == nil && matched
		})
	}

	index := map[jobKey]int{}
	var rows []JobRow
	row := func(k jobKey) *JobRow {
		i, ok := index[k]
		if !ok {
			i = len(rows)
			index[k] = i
			rows = append(rows, JobRow{Name: k.name, Repo: k.repo})
		}
		return &rows[i]
	}
	repoOf := map[string]string{}
	for _, c := range f.Failed {
		repoOf[c.Ticket] = c.Repo
		if !isRollup(c.Repo, c.Name) {
			row(jobKey{c.Name, c.Repo}).Failures++
		}
	}
	for _, fix := range f.Fixes {
		repo := repoOf[fix.Ticket]
		var names []string
		for _, name := range fix.Checks {
			if !isRollup(repo, name) {
				names = append(names, name)
			}
		}
		slices.Sort(names)
		names = slices.Compact(names)
		for i, name := range names {
			share := fix.Tokens / int64(len(names))
			if int64(i) < fix.Tokens%int64(len(names)) {
				share++
			}
			row(jobKey{name, repo}).FixTokens += share
		}
	}
	slices.SortFunc(rows, func(a, b JobRow) int {
		return cmp.Or(cmp.Compare(b.Failures, a.Failures), cmp.Compare(b.FixTokens, a.FixTokens),
			cmp.Compare(a.Name, b.Name), cmp.Compare(a.Repo, b.Repo))
	})
	return rows
}
