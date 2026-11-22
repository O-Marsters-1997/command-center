package view

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

const (
	factoryPath       = "/factory"
	factoryMergedCap  = 10
	factoryBlockedCap = 4
)

type FactoryParams struct {
	Params
	BlockedAll bool
}

type Factory struct {
	Chrome
	Scope       ScopeBar
	Counts      FactoryCounts
	Columns     []Column
	FloorPath   string
	PollSeconds int
}

type FactoryCounts struct {
	Working   int
	NeedsYou  int
	Reviewing int
	Merged    int
}

type Column struct {
	Stage  plan.Stage
	Glyph  plan.GlyphWord
	Title  string
	Empty  string
	Cards  []Card
	Total  int
	Hidden int
	More   string
	Fewer  string
	SeeAll string
}

type Card struct {
	Row
}

func (c Card) Flagged() bool {
	return c.Stage != plan.StageReview && (c.Glyph == plan.GlyphFailed || c.Glyph == plan.GlyphAttention)
}

var factoryColumns = []struct {
	Stage plan.Stage
	Glyph plan.GlyphWord
	Title string
	Empty string
}{
	{plan.StageBlocked, plan.GlyphBlocked, "Blocked", "Nothing waiting on a blocker"},
	{plan.StageReady, plan.GlyphReady, "Ready", "Nothing ready to launch"},
	{plan.StageWorking, plan.GlyphRunning, "Agent working", "No agents running"},
	{plan.StagePR, plan.GlyphChecking, "PR / CI", "No PRs in checks"},
	{plan.StageReview, plan.GlyphAttention, "Review me", "Nothing waiting on you"},
	{plan.StageMerged, plan.GlyphDone, "Merged", "Nothing merged yet"},
}

func (r *Reader) Factory(ctx context.Context, now time.Time, params FactoryParams) (Factory, error) {
	f, err := r.loadFrame(ctx, now, params.Params)
	if err != nil {
		return Factory{}, fmt.Errorf("load frame: %w", err)
	}
	rows, err := r.spentRows(ctx, f)
	if err != nil {
		return Factory{}, err
	}
	p := f.params
	columns, counts := buildFloor(rowsIn(scopeGroups(rows, p, f.project)), p, params.BlockedAll)
	chrome := f.chrome
	chrome.titled("Factory")
	chrome.Crumbs = []Crumb{{Label: "Factory"}}
	chrome.setCurrent(keyFactory)
	return Factory{
		Chrome:      chrome,
		Scope:       BuildScopeBar(rows, factoryQuery(p, false)),
		Counts:      counts,
		Columns:     columns,
		FloorPath:   withQuery(factoryPath, factoryQuery(p, params.BlockedAll).Encode()),
		PollSeconds: r.boardPollSeconds,
	}, nil
}

func factoryQuery(p Params, blockedAll bool) url.Values {
	q := url.Values{}
	for k, v := range map[string]string{"project": p.Project, "feature": p.Feature, "repo": p.Repo} {
		if v != "" {
			q.Set(k, v)
		}
	}
	if blockedAll {
		q.Set("blocked", "all")
	}
	return q
}

func buildFloor(rows []Row, p Params, blockedAll bool) ([]Column, FactoryCounts) {
	var counts FactoryCounts
	byStage := map[plan.Stage][]Card{}
	for _, r := range rows {
		if r.Stage == plan.StageNone {
			continue
		}
		c := Card{r}
		byStage[r.Stage] = append(byStage[r.Stage], c)
		if r.Alive {
			counts.Working++
		}
		if c.Flagged() {
			counts.NeedsYou++
		}
	}
	counts.Reviewing, counts.Merged = len(byStage[plan.StageReview]), len(byStage[plan.StageMerged])

	more := withQuery(factoryPath, factoryQuery(p, true).Encode())
	fewer := withQuery(factoryPath, factoryQuery(p, false).Encode())
	columns := make([]Column, len(factoryColumns))
	for i, def := range factoryColumns {
		cards := byStage[def.Stage]
		col := Column{Stage: def.Stage, Glyph: def.Glyph, Title: def.Title, Empty: def.Empty, Total: len(cards)}
		switch def.Stage {
		case plan.StageMerged:
			slices.SortStableFunc(cards, func(a, b Card) int { return cmp.Compare(b.Updated.Stamp, a.Updated.Stamp) })
			if len(cards) > factoryMergedCap {
				cards = cards[:factoryMergedCap]
				col.SeeAll = Params{
					Filter: SectionDone, Project: p.Project, Feature: p.Feature, Repo: p.Repo, All: true,
				}.PagePath()
			}
		case plan.StageBlocked:
			switch {
			case blockedAll && len(cards) > factoryBlockedCap:
				col.Fewer = fewer
			case !blockedAll && len(cards) > factoryBlockedCap:
				col.Hidden = len(cards) - factoryBlockedCap
				col.More = more
				cards = cards[:factoryBlockedCap]
			}
		case plan.StageNone, plan.StageReady, plan.StageWorking, plan.StagePR, plan.StageReview:
			slices.SortStableFunc(cards, func(a, b Card) int {
				if a.Flagged() != b.Flagged() {
					if a.Flagged() {
						return -1
					}
					return 1
				}
				return cmp.Compare(a.Updated.Stamp, b.Updated.Stamp)
			})
		}
		col.Cards = cards
		columns[i] = col
	}
	return columns, counts
}

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
		AllProjects: ScopeTab{
			Label:   "All projects",
			Path:    pickProject(""),
			Count:   unmergedCount(rows),
			Current: project == "",
		},
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
