package view

import (
	"cmp"
	"context"
	"fmt"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
)

const RailOpenSeparator = "|"
const ticketsPath = "/tickets"

type Home struct {
	Chrome
	Target string
}

func (r *Reader) Home(ctx context.Context, now time.Time) (Home, error) {
	f, err := r.loadFrame(ctx, now, Params{})
	if err != nil {
		return Home{}, fmt.Errorf("load frame: %w", err)
	}
	chrome := f.chrome
	rail := railFromFrame(f, RailParams{})
	chrome.Home = true
	home := Home{Chrome: chrome, Target: ticketsPath}
	if len(rail.NeedsYou) > 0 {
		home.Target = rail.NeedsYou[0].Path
	}
	return home, nil
}

const railSettledKey = "settled"

type Rail struct {
	NeedsYou []RailItem
	Features []RailFeature
	Settled  RailSettled
	Gauges   []Gauge
	Health   RailHealth
	Query    string
}

func (r Rail) NoMatch() bool {
	return r.Query != "" && len(r.NeedsYou) == 0 && len(r.Features) == 0 && len(r.Settled.Items) == 0
}

type RailItem struct {
	URL      string
	Ref      string
	Title    string
	Repo     string
	Feature  string
	Project  ProjectMark
	Context  string
	Glyph    plan.GlyphWord
	Reason   string
	Verb     plan.Verb
	Reply    bool
	Path     string
	Selected bool
	Started  string
}

type RailFeature struct {
	Key     string
	Name    string
	Open    bool
	Marks   []ProjectMark
	Live    []RailItem
	Blocked int
	Ready   int
	Merged  int
	Total   int
}

func (f RailFeature) Waiting() int { return f.Blocked + f.Ready }

func (f RailFeature) WaitingLine() string {
	var parts []string
	if f.Blocked > 0 {
		parts = append(parts, fmt.Sprintf("%d blocked", f.Blocked))
	}
	if f.Ready > 0 {
		parts = append(parts, fmt.Sprintf("%d ready", f.Ready))
	}
	return strings.Join(parts, ", ")
}

type RailSettled struct {
	Key   string
	Open  bool
	Items []RailItem
}

type RailHealth struct {
	Live    int
	Observe Age
	Stale   bool
	Error   option.Option[TickError]
}

type RailParams struct {
	Sel     string
	Open    []string
	Query   string
	Project string
	Repo    string

	ProjectRepos []string
}

func RailScope(currentURL string) (project, repo string) {
	u, err := url.Parse(currentURL)
	if err != nil {
		return "", ""
	}
	q := u.Query()
	return q.Get("project"), q.Get("repo")
}

func RailSelection(currentURL string) string {
	u, err := url.Parse(currentURL)
	if err != nil {
		return ""
	}
	if !strings.HasPrefix(u.Path, "/s/") {
		return ""
	}
	return u.Path
}

func (r *Reader) Rail(ctx context.Context, now time.Time, params RailParams) (Rail, error) {
	f, err := r.loadFrame(ctx, now, Params{Repo: params.Repo, Project: params.Project})
	if err != nil {
		return Rail{}, fmt.Errorf("load frame: %w", err)
	}
	return railFromFrame(f, params), nil
}

func railFromFrame(f frame, params RailParams) Rail {
	params.Repo = f.params.Repo
	params.Project, params.ProjectRepos = f.project.Name, f.project.Repos
	rail := BuildRail(f.rows, f.now, params)
	rail.Gauges = f.chrome.Gauges
	rail.Health = RailHealth{
		Live: f.chrome.LiveAgents, Observe: f.chrome.Observe, Stale: f.chrome.ObserveStale, Error: f.chrome.LastError,
	}
	return rail
}

func scopeRailRows(rows []Row, params RailParams) []Row {
	if params.Repo == "" && params.Project == "" {
		return rows
	}
	groups := FilterGroupsByRepo(groupRows(rows), params.Repo)
	if params.Project != "" {
		groups = FilterGroupsByProject(groups, params.ProjectRepos)
	}
	return rowsIn(groups)
}

func BuildRail(rows []Row, now time.Time, params RailParams) Rail {
	rows = scopeRailRows(rows, params)
	scope := Params{Project: params.Project}
	item := func(r Row) RailItem {
		_, featureName := railFeatureKey(r)
		it := RailItem{
			URL: r.URL, Ref: r.Ticket(), Title: r.Title, Repo: r.Repo, Feature: featureName, Glyph: r.Glyph,
			Project: r.Mark(), Context: railContext(r),
			Reason: r.Reason, Path: scope.withScope(cmp.Or(SessionPath(r.URL), ticketsPath)),
		}
		it.Selected = params.Sel != "" && params.Sel == SessionPath(r.URL)
		it.Reply = r.AwaitingInput() && r.FollowUpAvailable() && r.Reason != ""
		for _, verb := range r.Verbs {
			if !it.Reply && verb != plan.VerbFollowUp && verb != plan.VerbLaunch {
				it.Verb = verb
				break
			}
		}
		if r.Alive && r.Glyph == plan.GlyphRunning {
			it.Started = now.Add(-time.Duration(r.ElapsedSeconds) * time.Second).UTC().Format(time.RFC3339)
		}
		return it
	}
	open := func(key string) bool { return slices.Contains(params.Open, key) }
	byRepoThenRef := func(a, b RailItem) int {
		return cmp.Or(cmp.Compare(a.Repo, b.Repo), cmp.Compare(refNumber(a.URL), refNumber(b.URL)), cmp.Compare(a.URL, b.URL))
	}

	var rail Rail
	byFeature := map[string][]Row{}
	for _, r := range rows {
		group, _ := plan.RailGroup(r.Glyph)
		switch {
		case group == plan.GroupNeedsYou:
			rail.NeedsYou = append(rail.NeedsYou, item(r))
		case group == plan.GroupSettled && r.Worktree != "":
			rail.Settled.Items = append(rail.Settled.Items, item(r))
		}
		key, _ := railFeatureKey(r)
		byFeature[key] = append(byFeature[key], r)
	}

	slices.SortFunc(rail.NeedsYou, func(a, b RailItem) int {
		return cmp.Or(cmp.Compare(glyphRank(a.Glyph), glyphRank(b.Glyph)), byRepoThenRef(a, b))
	})
	slices.SortFunc(rail.Settled.Items, byRepoThenRef)
	rail.Settled.Key = railSettledKey
	rail.Settled.Open = open(railSettledKey)

	for key, members := range byFeature {
		_, name := railFeatureKey(members[0])
		f := RailFeature{Key: key, Name: name, Open: open(key)}
		f.Total = len(members)
		for _, r := range members {
			if mark := r.Mark(); mark.Name != "" && !slices.Contains(f.Marks, mark) {
				f.Marks = append(f.Marks, mark)
			}
			switch r.Glyph {
			case plan.GlyphReady:
				f.Ready++
			case plan.GlyphBlocked:
				f.Blocked++
			case plan.GlyphDone:
				f.Merged++
			case plan.GlyphUnknown, plan.GlyphFailed, plan.GlyphAttention, plan.GlyphRunning,
				plan.GlyphPending, plan.GlyphChecking:
			}
			if group, folded := plan.RailGroup(r.Glyph); group == plan.GroupInFlight && !folded {
				f.Live = append(f.Live, item(r))
			}
		}
		if len(f.Live) == 0 {
			continue
		}
		slices.SortFunc(f.Live, byRepoThenRef)
		slices.SortFunc(f.Marks, compareMarks)
		rail.Features = append(rail.Features, f)
	}
	slices.SortFunc(rail.Features, func(a, b RailFeature) int {
		return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.Key, b.Key))
	})
	return filterRail(rail, params.Query)
}

func filterRail(rail Rail, query string) Rail {
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return rail
	}
	keep := func(items []RailItem) []RailItem {
		return slices.DeleteFunc(slices.Clone(items), func(it RailItem) bool {
			return !matchesAll(terms, it.Ref, strings.TrimPrefix(it.Ref, "#"), it.Title, it.Repo, it.Feature)
		})
	}

	rail.Query = strings.TrimSpace(query)
	rail.NeedsYou = keep(rail.NeedsYou)

	var features []RailFeature
	for _, f := range rail.Features {
		if !matchesAll(terms, f.Name) {
			f.Live = keep(f.Live)
		}
		if len(f.Live) == 0 {
			continue
		}
		f.Open = true
		features = append(features, f)
	}
	rail.Features = features

	rail.Settled.Items = keep(rail.Settled.Items)
	rail.Settled.Open = true
	return rail
}

func matchesAll(terms []string, fields ...string) bool {
	return !slices.ContainsFunc(terms, func(term string) bool {
		return !slices.ContainsFunc(fields, func(field string) bool {
			return strings.Contains(strings.ToLower(field), term)
		})
	})
}

func railContext(r Row) string {
	parts := []string{cmp.Or(r.Project, r.Repo)}
	if r.Feature != "" {
		parts = append(parts, r.Feature)
	}
	return strings.Join(parts, " · ") + " " + r.Ticket()
}

func railFeatureKey(r Row) (key, name string) {
	if r.Feature == "" {
		return "repo:" + r.Repo, r.Repo
	}
	return "feature:" + r.Feature, r.Feature
}

func glyphRank(glyph plan.GlyphWord) int {
	if glyph == plan.GlyphFailed {
		return 0
	}
	return 1
}

func refNumber(ticketURL string) int {
	n, err := strconv.Atoi(path.Base(ticketURL))
	if err != nil {
		return 0
	}
	return n
}
