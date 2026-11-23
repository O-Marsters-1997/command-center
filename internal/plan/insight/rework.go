package insight

import (
	"cmp"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"
)

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
