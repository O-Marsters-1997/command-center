package view

import (
	"maps"
	"net/url"
	"slices"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

const factoryPath = "/factory"

type ScopeBar struct {
	AllProjects ScopeTab
	Projects    []ScopeTab
	Features    []ScopeTab
	Repos       []ScopeTab
	Clear       string
}

type ScopeTab struct {
	Label   string
	Path    string
	Count   int
	Current bool
	Mark    ProjectMark
}

func BuildScopeBar(rows []Row, q url.Values) ScopeBar {
	project, feature, repo := q.Get("project"), q.Get("feature"), q.Get("repo")
	pathWith := func(set map[string]string) string {
		next := url.Values{}
		for k, vs := range q {
			if vs = slices.DeleteFunc(slices.Clone(vs), func(v string) bool { return v == "" }); len(vs) > 0 {
				next[k] = vs
			}
		}
		for k, v := range set {
			next.Del(k)
			if v != "" {
				next.Set(k, v)
			}
		}
		return withQuery(factoryPath, next.Encode())
	}
	pickProject := func(v string) string {
		return pathWith(map[string]string{"project": v, "feature": "", "repo": "", "closed": ""})
	}
	toggle := func(key, current string) func(string) string {
		return func(value string) string {
			if value == current {
				value = ""
			}
			return pathWith(map[string]string{key: value})
		}
	}

	inProject := rows
	if project != "" {
		inProject = slices.DeleteFunc(slices.Clone(rows), func(r Row) bool { return r.Project != project })
	}
	bar := ScopeBar{
		AllProjects: ScopeTab{Label: "All projects", Path: pickProject(""), Count: unmergedCount(rows), Current: project == ""},
		Projects: scopeTabs(rows, func(r Row) (string, string) { return r.Project, r.Project }, project,
			func(v string) string {
				if v == project {
					v = ""
				}
				return pickProject(v)
			}),
		Features: scopeTabs(inProject, func(r Row) (string, string) { return r.Feature, r.FeatureTitle() }, feature,
			toggle("feature", feature)),
		Repos: scopeTabs(inProject, func(r Row) (string, string) { return r.Repo, r.Repo }, repo,
			toggle("repo", repo)),
	}
	if feature != "" || repo != "" {
		bar.Clear = pathWith(map[string]string{"feature": "", "repo": ""})
	}
	return bar
}

func unmergedCount(rows []Row) int {
	n := 0
	for _, r := range rows {
		if r.State != plan.PRMerged {
			n++
		}
	}
	return n
}

func scopeTabs(rows []Row, of func(Row) (value, label string), current string, pathFor func(string) string) []ScopeTab {
	tabs := map[string]ScopeTab{}
	for _, r := range rows {
		value, label := of(r)
		if value == "" {
			continue
		}
		tab, ok := tabs[value]
		if !ok {
			tab = ScopeTab{Label: label, Mark: r.Mark(), Path: pathFor(value), Current: value == current}
		}
		if r.State != plan.PRMerged {
			tab.Count++
		}
		tabs[value] = tab
	}
	out := slices.Collect(maps.Values(tabs))
	slices.SortFunc(out, func(a, b ScopeTab) int { return compareFold(a.Label, b.Label) })
	return out
}
