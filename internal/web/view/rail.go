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
)

const RailOpenSeparator = "|"

const railSettledKey = "settled"

type Rail struct {
	NeedsYou []RailItem
	Features []RailFeature
	Settled  RailSettled
	Gauges   []Gauge
	Health   RailHealth
}

type RailItem struct {
	URL      string
	Ref      string
	Title    string
	Repo     string
	Glyph    string
	Reason   string
	Verb     string
	Path     string
	Selected bool
	Started  string
}

type RailFeature struct {
	Key     string
	Name    string
	Open    bool
	Live    []RailItem
	Blocked int
	Ready   int
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
	Error   *TickError
}

type RailParams struct {
	Sel  string
	Open []string
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
	tickets, err := r.store.Tickets(ctx)
	if err != nil {
		return Rail{}, err
	}
	in, err := r.store.PlanInput(ctx)
	if err != nil {
		return Rail{}, err
	}
	lastErr, failed, err := r.store.LastError(ctx)
	if err != nil {
		return Rail{}, err
	}
	gauges, err := r.store.LatestReadings(ctx)
	if err != nil {
		return Rail{}, err
	}
	split, err := r.gaugeSplit(ctx, now)
	if err != nil {
		return Rail{}, err
	}
	in.Now = now
	rows := deriveRows(tickets, in, plan.RulesFor(plan.Daemon{}, in.Obs).Derive(in))
	chrome := r.buildChrome(tickets, in.Obs, in.Observed, lastErr, failed, gauges, split, now, Params{})

	rail := buildRail(rows, now, params)
	rail.Gauges = chrome.Gauges
	rail.Health = RailHealth{
		Live: chrome.LiveAgents, Observe: chrome.Observe, Stale: chrome.ObserveStale, Error: chrome.LastError,
	}
	return rail, nil
}

func buildRail(rows []Row, now time.Time, params RailParams) Rail {
	item := func(r Row) RailItem {
		it := RailItem{
			URL: r.URL, Ref: r.Ticket(), Title: r.Title, Repo: r.Repo, Glyph: r.Glyph, Reason: r.Reason,
			Path: cmp.Or(SessionPath(r.URL), ticketsPath),
		}
		it.Selected = params.Sel != "" && params.Sel == SessionPath(r.URL)
		for _, verb := range r.Verbs {
			if verb != plan.VerbFollowUp && verb != plan.VerbLaunch {
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
		for _, r := range members {
			switch r.Glyph {
			case plan.GlyphReady:
				f.Ready++
			case plan.GlyphBlocked:
				f.Blocked++
			}
			if group, folded := plan.RailGroup(r.Glyph); group == plan.GroupInFlight && !folded {
				f.Live = append(f.Live, item(r))
			}
		}
		if len(f.Live) == 0 {
			continue
		}
		slices.SortFunc(f.Live, byRepoThenRef)
		rail.Features = append(rail.Features, f)
	}
	slices.SortFunc(rail.Features, func(a, b RailFeature) int {
		return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.Key, b.Key))
	})
	return rail
}

func railFeatureKey(r Row) (key, name string) {
	if r.Feature == "" {
		return "repo:" + r.Repo, r.Repo
	}
	return "feature:" + r.Feature, r.Feature
}

func glyphRank(glyph string) int {
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
