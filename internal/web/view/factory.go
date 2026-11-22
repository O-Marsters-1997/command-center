package view

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

const (
	factoryPath       = "/factory"
	factoryMergedCap  = 10
	factoryBlockedCap = 4
)

type LaneBy string

const (
	LaneByNone    LaneBy = ""
	LaneByProject LaneBy = "project"
	LaneByFeature LaneBy = "feature"
	LaneByRepo    LaneBy = "repo"
)

type FactoryParams struct {
	Params
	BlockedAll bool
	By         LaneBy
	Closed     []string
}

type Factory struct {
	Chrome
	Scope       ScopeBar
	Counts      FactoryCounts
	Lanes       []Lane
	LaneTabs    []ScopeTab
	ExpandAll   string
	CollapseAll string
	FloorPath   string
	PollSeconds int
}

type Lane struct {
	Key      string
	Name     string
	Mark     ProjectMark
	Columns  []Column
	Segments []LaneSegment
	Count    int
	Working  int
	NeedsYou int
	Open     bool
	Toggle   string
}

type LaneSegment struct {
	Glyph plan.GlyphWord
	Title string
	Count int
}

func (by LaneBy) lane(r Row) (key, name string) {
	switch by {
	case LaneByProject:
		return "p:" + r.Project, cmp.Or(r.Project, "No project")
	case LaneByFeature:
		return "f:" + r.Feature, cmp.Or(strings.TrimPrefix(r.Feature, "project:"), "No feature")
	case LaneByRepo:
		return "r:" + r.Repo, cmp.Or(r.Repo, "No repo")
	case LaneByNone:
	}
	return "", ""
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
	fp := params
	fp.Params = f.params
	switch fp.By {
	case LaneByProject, LaneByFeature, LaneByRepo, LaneByNone:
	default:
		fp.By = LaneByNone
	}
	lanes, counts := buildFloor(rowsIn(scopeGroups(rows, fp.Params, f.project)), fp)
	chrome := f.chrome
	chrome.titled("Factory")
	chrome.Crumbs = []Crumb{{Label: "Factory"}}
	chrome.setCurrent(keyFactory)
	page := Factory{
		Chrome:      chrome,
		Scope:       BuildScopeBar(rows, fp.query(false, fp.Closed)),
		Counts:      counts,
		Lanes:       lanes,
		LaneTabs:    fp.laneTabs(),
		FloorPath:   fp.path(fp.BlockedAll, fp.Closed),
		PollSeconds: r.boardPollSeconds,
	}
	if fp.By != LaneByNone {
		page.ExpandAll = fp.path(fp.BlockedAll, nil)
		keys := make([]string, len(lanes))
		for i, l := range lanes {
			keys[i] = l.Key
		}
		page.CollapseAll = fp.path(fp.BlockedAll, keys)
	}
	return page, nil
}

func (fp FactoryParams) query(blockedAll bool, closed []string) url.Values {
	q := url.Values{}
	scope := map[string]string{"project": fp.Project, "feature": fp.Feature, "repo": fp.Repo, "by": string(fp.By)}
	for k, v := range scope {
		if v != "" {
			q.Set(k, v)
		}
	}
	if blockedAll {
		q.Set("blocked", "all")
	}
	if len(closed) > 0 {
		q["closed"] = closed
	}
	return q
}

func (fp FactoryParams) path(blockedAll bool, closed []string) string {
	return withQuery(factoryPath, fp.query(blockedAll, closed).Encode())
}

func (fp FactoryParams) laneTabs() []ScopeTab {
	var tabs []ScopeTab
	for _, t := range []struct {
		by    LaneBy
		label string
	}{{LaneByNone, "None"}, {LaneByProject, "Project"}, {LaneByFeature, "Feature"}, {LaneByRepo, "Repo"}} {
		next := fp
		next.By = t.by
		tabs = append(tabs, ScopeTab{Label: t.label, Path: next.path(fp.BlockedAll, fp.Closed), Current: t.by == fp.By})
	}
	return tabs
}

func (fp FactoryParams) toggled(key string) []string {
	if slices.Contains(fp.Closed, key) {
		return slices.DeleteFunc(slices.Clone(fp.Closed), func(k string) bool { return k == key })
	}
	return append(slices.Clone(fp.Closed), key)
}

type laneBuild struct {
	Lane
	byStage map[plan.Stage][]Card
}

func buildFloor(rows []Row, fp FactoryParams) ([]Lane, FactoryCounts) {
	var counts FactoryCounts
	builds := map[string]*laneBuild{}
	newBuild := func(key, name string, mark ProjectMark) *laneBuild {
		b := &laneBuild{Lane: Lane{Key: key, Name: name, Mark: mark}, byStage: map[plan.Stage][]Card{}}
		builds[key] = b
		return b
	}
	if fp.By == LaneByNone {
		newBuild("", "", ProjectMark{})
	}
	for _, r := range rows {
		if r.Stage == plan.StageNone {
			continue
		}
		key, name := fp.By.lane(r)
		b, ok := builds[key]
		if !ok {
			mark := r.Mark()
			if fp.By == LaneByRepo {
				mark = ProjectMark{}
			}
			b = newBuild(key, name, mark)
		}
		c := Card{r}
		b.byStage[r.Stage] = append(b.byStage[r.Stage], c)
		b.Count++
		if r.Alive {
			b.Working++
			counts.Working++
		}
		if c.Flagged() {
			b.NeedsYou++
			counts.NeedsYou++
		}
		switch r.Stage {
		case plan.StageReview:
			counts.Reviewing++
		case plan.StageMerged:
			counts.Merged++
		case plan.StageNone, plan.StageBlocked, plan.StageReady, plan.StageWorking, plan.StagePR:
		}
	}

	lanes := make([]Lane, 0, len(builds))
	for _, b := range builds {
		b.Open = b.Key == "" || !slices.Contains(fp.Closed, b.Key)
		b.Toggle = fp.path(fp.BlockedAll, fp.toggled(b.Key))
		for _, def := range factoryColumns {
			if n := len(b.byStage[def.Stage]); n > 0 {
				b.Segments = append(b.Segments, LaneSegment{Glyph: def.Glyph, Title: def.Title, Count: n})
			}
		}
		if b.Open {
			b.Columns = floorColumns(b.byStage, fp)
		}
		lanes = append(lanes, b.Lane)
	}
	slices.SortFunc(lanes, func(a, b Lane) int {
		return cmp.Or(compareFold(a.Name, b.Name), cmp.Compare(a.Key, b.Key))
	})
	return lanes, counts
}

func floorColumns(byStage map[plan.Stage][]Card, fp FactoryParams) []Column {
	blockedAll := fp.BlockedAll
	more := fp.path(true, fp.Closed)
	fewer := fp.path(false, fp.Closed)
	p := fp.Params
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
	return columns
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
