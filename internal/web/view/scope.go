package view

import (
	"cmp"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/O-Marsters-1997/command-center/internal/store"
)

const (
	ScopeProject = "project"
	ScopeRepo    = "repo"
)

type ScopeEntry struct {
	Label   string
	Kind    string
	Value   string
	Hue     int
	Initial string
	Path    string
}

func ScopeList(projects []store.Project, repos []store.Repo) []ScopeEntry {
	entries := make([]ScopeEntry, 0, len(projects)+len(repos))
	for _, p := range projects {
		entries = append(entries, ScopeEntry{
			Label: p.Name, Kind: ScopeProject, Value: p.Name, Hue: p.Hue,
			Initial: initial(p.Name), Path: "/tickets?project=" + url.QueryEscape(p.Name),
		})
	}
	slices.SortFunc(entries, func(a, b ScopeEntry) int {
		return compareFold(a.Label, b.Label)
	})
	var loose []ScopeEntry
	for _, r := range repos {
		if r.Project != "" {
			continue
		}
		loose = append(loose, ScopeEntry{
			Label: r.Name, Kind: ScopeRepo, Value: r.Name, Path: "/tickets?repo=" + url.QueryEscape(r.Name),
		})
	}
	slices.SortFunc(loose, func(a, b ScopeEntry) int { return strings.Compare(a.Label, b.Label) })
	return append(entries, loose...)
}

type ProjectMark struct {
	Name string
	Hue  int
}

func (r Row) Mark() ProjectMark { return ProjectMark{Name: r.Project, Hue: r.ProjectHue} }

func (m ProjectMark) Initial() string { return initial(m.Name) }

func compareMarks(a, b ProjectMark) int {
	return cmp.Or(compareFold(a.Name, b.Name), cmp.Compare(a.Name, b.Name))
}

func compareFold(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) }

func initial(name string) string {
	r, _ := utf8.DecodeRuneInString(name)
	return strings.ToUpper(string(r))
}

func hues() []int {
	var all []int
	for hue := store.MinHue; hue <= store.MaxHue; hue++ {
		all = append(all, hue)
	}
	return all
}

func leastUsedHue(projects []store.Project) int {
	var uses [store.MaxHue + 1]int
	for _, p := range projects {
		uses[p.Hue]++
	}
	best := store.MinHue
	for hue := store.MinHue; hue <= store.MaxHue; hue++ {
		if uses[hue] < uses[best] {
			best = hue
		}
	}
	return best
}
