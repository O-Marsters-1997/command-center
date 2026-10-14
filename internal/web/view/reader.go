package view

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/option"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/spend"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

const observeStaleAfter = 20 * time.Second

type Board struct {
	Chrome
	Groups           []Group
	Sections         []Section
	Tabs             []FilterTab
	BoardPath        string
	BoardPollSeconds int
	Launched         option.Option[Age]
	TicketCount      int
	WaveCount        int
	// Launch is set on /f/{feature}/launch, where the page opens with the launch dialog over it.
	Launch option.Option[LaunchModal]
}

func (b Board) Row(ticketURL string) (Row, bool) {
	for _, g := range b.Groups {
		if g.Root.IsSome() && g.Root.Unwrap().URL == ticketURL {
			return g.Root.Unwrap(), true
		}
		for _, child := range g.Children {
			if child.URL == ticketURL {
				return child, true
			}
		}
	}
	return Row{}, false
}

// InvalidError marks a request the caller got wrong, as opposed to a read that failed.
type InvalidError struct{ Err error }

func (e InvalidError) Error() string { return e.Err.Error() }
func (e InvalidError) Unwrap() error { return e.Err }

func IsInvalid(err error) bool {
	var invalid InvalidError
	return errors.As(err, &invalid)
}

// Reader reads the store, derives plan's snapshot and shapes it for the pages. It imports neither
// net/http nor html/template: handlers parse the request, call a Reader, then render or queue an
// intent.
type Reader struct {
	store            *store.Store
	dataDir          string
	spendLimit5h     int
	boardPollSeconds int
	spend            *SpendCache
	renderLine       LineRenderer
}

func NewReader(
	st *store.Store, dataDir string, renderLine LineRenderer, boardPollSeconds, spendLimit5h int,
) *Reader {
	return &Reader{
		store: st, dataDir: dataDir, renderLine: renderLine,
		boardPollSeconds: boardPollSeconds,
		spendLimit5h:     spendLimit5h,
		spend:            NewSpendCache(),
	}
}

func (r *Reader) Snapshot(ctx context.Context, now time.Time) (plan.Snapshot, error) {
	in, err := r.store.PlanInput(ctx)
	if err != nil {
		return plan.Snapshot{}, fmt.Errorf("load plan input: %w", err)
	}
	in.Now = now
	return plan.RulesFor(plan.Daemon{}, in.Obs).Derive(in), nil
}

// Board derives the board page. The repo and feature scopes narrow the groups after grouping, so
// a group with a member in scope stays whole.
func (r *Reader) Board(ctx context.Context, now time.Time, params Params) (Board, error) {
	tickets, err := r.store.Tickets(ctx)
	if err != nil {
		return Board{}, fmt.Errorf("load tickets: %w", err)
	}
	if params.Repo, err = r.repoScope(ctx, params.Repo); err != nil {
		return Board{}, fmt.Errorf("load repo scope: %w", err)
	}
	project, err := r.projectScope(ctx, params.Project)
	if err != nil {
		return Board{}, fmt.Errorf("load project scope: %w", err)
	}
	params.Project = project.Name
	params.Feature = normalizeFeatureScope(params.Feature, distinctFeatures(tickets))
	in, err := r.store.PlanInput(ctx)
	if err != nil {
		return Board{}, fmt.Errorf("load plan input: %w", err)
	}
	lastErr, err := r.store.LastError(ctx)
	if err != nil {
		return Board{}, fmt.Errorf("load last error: %w", err)
	}
	gauges, err := r.store.LatestReadings(ctx)
	if err != nil {
		return Board{}, fmt.Errorf("load latest readings: %w", err)
	}

	in.Now = now
	split, err := r.gaugeSplit(ctx, now)
	if err != nil {
		return Board{}, fmt.Errorf("gauge split: %w", err)
	}
	ticketSpend, err := r.store.BoardTicketSpend(ctx, params.Repo, params.Feature)
	if err != nil {
		return Board{}, fmt.Errorf("load board ticket spend: %w", err)
	}

	projects, err := r.projectMarks(ctx)
	if err != nil {
		return Board{}, fmt.Errorf("load project marks: %w", err)
	}
	rows := deriveRows(tickets, in, plan.RulesFor(plan.Daemon{}, in.Obs).Derive(in), projects)
	if err := applySpend(rows, r.spend); err != nil {
		return Board{}, fmt.Errorf("apply spend: %w", err)
	}
	applyTicketSpend(rows, ticketSpend, split[agentlog.SevenDay].Fit.Factor)
	var records []store.Event
	if params.Sel != "" {
		if records, err = r.store.TicketEventsAfter(ctx, params.Sel, 0); err != nil {
			return Board{}, fmt.Errorf("load ticket events after: %w", err)
		}
	}
	if err := applyViewState(rows, params, records, r.renderLine); err != nil {
		return Board{}, fmt.Errorf("apply view state: %w", err)
	}
	groups := filterGroupsByFeature(filterGroupsByRepo(groupRows(rows), params.Repo), params.Feature)
	if project.Name != "" {
		groups = filterGroupsByProject(groups, project.Repos)
	}
	chrome := r.buildChrome(chromeInput{tickets, in.Obs, in.Observed, lastErr, gauges, split}, now, params)
	if err := r.scopeChrome(ctx, &chrome, chromeScope{tickets, project, params.Repo}); err != nil {
		return Board{}, fmt.Errorf("scope chrome: %w", err)
	}
	scoped := rowsIn(groups)
	sections := Sectioned(scoped)
	blocking := make(map[plan.TicketURL][]plan.TicketURL, len(scoped))
	for _, row := range scoped {
		blocking[plan.TicketURL(row.URL)] = plan.TicketURLs(row.BlockedBy)
	}
	launched := option.None[Age]()
	if params.Feature != "" {
		at, ok, err := r.store.FeatureFirstLaunch(ctx, params.Feature)
		if err != nil {
			return Board{}, fmt.Errorf("load feature first launch: %w", err)
		}
		if ok {
			launched = option.Some(relative(now, at))
		}
	}
	return Board{
		Launched:         launched,
		TicketCount:      len(scoped),
		WaveCount:        plan.WaveCount(blocking),
		Chrome:           chrome,
		Groups:           groups,
		Sections:         filterSections(sections, params.Filter),
		Tabs:             filterTabs(sections, params),
		BoardPath:        params.boardPath(),
		BoardPollSeconds: r.boardPollSeconds,
	}, nil
}

func (r *Reader) Chrome(ctx context.Context, now time.Time, params Params) (Chrome, error) {
	tickets, err := r.store.Tickets(ctx)
	if err != nil {
		return Chrome{}, fmt.Errorf("load tickets: %w", err)
	}
	if params.Repo, err = r.repoScope(ctx, params.Repo); err != nil {
		return Chrome{}, fmt.Errorf("load repo scope: %w", err)
	}
	project, err := r.projectScope(ctx, params.Project)
	if err != nil {
		return Chrome{}, fmt.Errorf("load project scope: %w", err)
	}
	params.Project = project.Name
	params.Feature = normalizeFeatureScope(params.Feature, distinctFeatures(tickets))

	obs, observed, err := r.store.LastObservation(ctx)
	if err != nil {
		return Chrome{}, fmt.Errorf("load last observation: %w", err)
	}
	lastErr, err := r.store.LastError(ctx)
	if err != nil {
		return Chrome{}, fmt.Errorf("load last error: %w", err)
	}
	gauges, err := r.store.LatestReadings(ctx)
	if err != nil {
		return Chrome{}, fmt.Errorf("load latest readings: %w", err)
	}
	split, err := r.gaugeSplit(ctx, now)
	if err != nil {
		return Chrome{}, fmt.Errorf("gauge split: %w", err)
	}
	chrome := r.buildChrome(chromeInput{tickets, obs, observed, lastErr, gauges, split}, now, params)
	if err := r.scopeChrome(ctx, &chrome, chromeScope{tickets, project, params.Repo}); err != nil {
		return Chrome{}, fmt.Errorf("scope chrome: %w", err)
	}
	return chrome, nil
}

type chromeScope struct {
	tickets []store.Ticket
	project store.Project
	repo    string
}

func (r *Reader) scopeChrome(ctx context.Context, c *Chrome, scope chromeScope) error {
	tickets, project, repo := scope.tickets, scope.project, scope.repo
	c.scopeToProject(project)
	projects, err := r.store.Projects(ctx)
	if err != nil {
		return fmt.Errorf("load projects: %w", err)
	}
	repos, err := r.store.Repos(ctx)
	if err != nil {
		return fmt.Errorf("load repos: %w", err)
	}
	c.ScopeMenu = scopeChoices(ScopeList(projects, repos), project.Name, repo)
	if c.RefusedRepos, err = r.refusedRepos(ctx); err != nil {
		return fmt.Errorf("load refused repos: %w", err)
	}
	in, err := r.store.PlanInput(ctx)
	if err != nil {
		return fmt.Errorf("load plan input: %w", err)
	}
	rows := deriveRows(tickets, in, plan.RulesFor(plan.Daemon{}, in.Obs).Derive(in), nil)
	rows = scopeRailRows(rows, RailParams{Project: project.Name, Repo: repo, projectRepos: project.Repos})
	for _, row := range rows {
		if row.Glyph != plan.GlyphDone {
			c.OpenTickets++
		}
	}
	return nil
}

func (r *Reader) projectMarks(ctx context.Context) (map[string]ProjectMark, error) {
	repos, err := r.store.Repos(ctx)
	if err != nil {
		return nil, fmt.Errorf("load repos: %w", err)
	}
	marks := make(map[string]ProjectMark, len(repos))
	for _, repo := range repos {
		if repo.Project != "" {
			marks[repo.Name] = ProjectMark{Name: repo.Project, Hue: repo.Hue}
		}
	}
	return marks, nil
}

func (r *Reader) refusedRepos(ctx context.Context) ([]RefusedRepo, error) {
	repos, err := r.store.Repos(ctx)
	if err != nil {
		return nil, fmt.Errorf("load repos: %w", err)
	}
	var refused []RefusedRepo
	for _, repo := range repos {
		if repo.Phase == plan.RepoRefused {
			refused = append(refused, RefusedRepo{Name: repo.Name, Reason: repo.Refusal, Path: RepoPath(repo.Name)})
		}
	}
	return refused, nil
}

type chromeInput struct {
	tickets  []store.Ticket
	obs      plan.Observation
	observed bool
	lastErr  option.Option[store.TickError]
	gauges   map[agentlog.Window]store.Gauge
	split    map[agentlog.Window]windowSplit
}

func (r *Reader) buildChrome(in chromeInput, now time.Time, params Params) Chrome {
	tickets, obs, observed, lastErr, gauges, split := in.tickets, in.obs, in.observed, in.lastErr, in.gauges, in.split
	c := Chrome{
		Workspace:    workspaceName(r.dataDir),
		LiveAgents:   liveAgents(tickets, obs),
		Observe:      Age{Age: "never"},
		ObserveStale: true,
		Gauges:       deriveGauges(gauges, split),
		Section:      "tickets",
		Keymap:       Keymap,
		RepoScope:    params.Repo,
		RepoCrumb:    params.Repo,
		FeatureScope: params.Feature,
		TicketsPath:  params.withScope(ticketsPath),
		ReposPath:    params.withScope("/repos"),
		LaunchPath:   params.withScope("/launch"),
		InsightsPath: withQuery("/insights", Params{Feature: params.Feature, Project: params.Project}.query()),
	}
	if fiveHour := gauges[agentlog.FiveHour].Utilization; spend.Paused(fiveHour, r.spendLimit5h) {
		c.SpendPaused = option.Some(SpendPaused{Pct: spend.Pct(fiveHour), Limit: r.spendLimit5h})
	}
	if observed {
		c.Observe = relative(now, obs.ObservedAt)
		c.ObserveStale = now.Sub(obs.ObservedAt) >= observeStaleAfter
	}
	if lastErr.IsSome() && (!observed || lastErr.Unwrap().At.After(obs.ObservedAt)) {
		c.LastError = option.Some(TickError{Age: relative(now, lastErr.Unwrap().At), Message: lastErr.Unwrap().Message})
	}
	if params.Repo != "" {
		c.RepoCrumbPath = RepoPath(params.Repo)
	}
	if params.Feature != "" {
		c.FeatureQuery = url.QueryEscape(params.Feature)
	}
	return c
}

func (r *Reader) gaugeSplit(ctx context.Context, now time.Time) (map[agentlog.Window]windowSplit, error) {
	fits, err := r.store.FitFactors(ctx, now)
	if err != nil {
		return nil, fmt.Errorf("load fit factors: %w", err)
	}
	ccCost, err := r.store.CCCostUSD(ctx, now)
	if err != nil {
		return nil, fmt.Errorf("load cc cost usd: %w", err)
	}
	split := make(map[agentlog.Window]windowSplit, len(fits))
	for window, fit := range fits {
		split[window] = windowSplit{Fit: fit, CCUSD: ccCost[window]}
	}
	return split, nil
}

func relative(now, then time.Time) Age {
	return Age{
		Age:   now.Sub(then).Round(time.Second).String() + " ago",
		Stamp: then.UTC().Format(time.RFC3339),
	}
}

func liveAgents(tickets []store.Ticket, obs plan.Observation) int {
	live := 0
	for _, t := range tickets {
		if obs.Runs[plan.TicketURL(t.URL)].Alive {
			live++
		}
	}
	return live
}

func workspaceName(dataDir string) string {
	if dataDir == "" {
		return ""
	}
	return filepath.Base(dataDir)
}
