package view

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/option"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/spend"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

const observeStaleAfter = 20 * time.Second
const eventPageSize = 500

func EachTicketEvent(
	ctx context.Context, st *store.Store, ticketURL string, afterID int64, fn func(store.Event) error,
) error {
	for {
		page, err := st.TicketEventsAfter(ctx, ticketURL, afterID, eventPageSize)
		if err != nil {
			return fmt.Errorf("read ticket events for %s: %w", ticketURL, err)
		}
		for _, e := range page {
			if err := fn(e); err != nil {
				return err
			}
			afterID = e.ID
		}
		if len(page) < eventPageSize {
			return nil
		}
	}
}

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
	Launch           option.Option[LaunchModal]
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

type InvalidError struct{ Err error }

func (e InvalidError) Error() string { return e.Err.Error() }
func (e InvalidError) Unwrap() error { return e.Err }

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

func (r *Reader) Board(ctx context.Context, now time.Time, params Params) (Board, error) {
	f, err := r.loadFrame(ctx, now, params)
	if err != nil {
		return Board{}, fmt.Errorf("load frame: %w", err)
	}
	params = f.params
	ticketSpend, err := r.store.BoardTicketSpend(ctx, params.Repo, params.Feature)
	if err != nil {
		return Board{}, fmt.Errorf("load board ticket spend: %w", err)
	}
	rows := slices.Clone(f.rows)
	if err := applySpend(rows, r.spend); err != nil {
		return Board{}, fmt.Errorf("apply spend: %w", err)
	}
	ApplyTicketSpend(rows, ticketSpend, f.split[agentlog.SevenDay].Fit.Factor)
	var records []store.Event
	if params.Sel != "" {
		err = EachTicketEvent(ctx, r.store, params.Sel, 0, func(e store.Event) error {
			records = append(records, e)
			return nil
		})
		if err != nil {
			return Board{}, fmt.Errorf("load ticket events after: %w", err)
		}
	}
	if err := applyViewState(rows, params, records, r.renderLine); err != nil {
		return Board{}, fmt.Errorf("apply view state: %w", err)
	}
	groups := FilterGroupsByFeature(FilterGroupsByRepo(groupRows(rows), params.Repo), params.Feature)
	if f.project.Name != "" {
		groups = FilterGroupsByProject(groups, f.project.Repos)
	}
	chrome := f.chrome
	chrome.titled(cmp.Or(params.Feature, f.project.Name, params.Repo, "Tickets"))
	scoped := rowsIn(groups)
	sections := Sectioned(scoped)
	blocking := make(map[plan.TicketURL][]plan.TicketURL, len(scoped))
	for _, row := range scoped {
		blocking[plan.TicketURL(row.URL)] = plan.TicketURLs(row.BlockedBy)
	}
	launched := option.None[Age]()
	if params.Feature != "" {
		at, err := r.store.FeatureFirstLaunch(ctx, params.Feature)
		switch {
		case err == nil:
			launched = option.Some(relative(now, at))
		case !errors.Is(err, store.ErrNotFound):
			return Board{}, fmt.Errorf("load feature first launch: %w", err)
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
		BoardPath:        params.BoardPath(),
		BoardPollSeconds: r.boardPollSeconds,
	}, nil
}

func (r *Reader) Chrome(ctx context.Context, now time.Time, params Params) (Chrome, error) {
	f, err := r.loadFrame(ctx, now, params)
	if err != nil {
		return Chrome{}, fmt.Errorf("load frame: %w", err)
	}
	return f.chrome, nil
}
func projectMarks(repos []store.Repo) map[string]ProjectMark {
	marks := make(map[string]ProjectMark, len(repos))
	for _, repo := range repos {
		if repo.Project != "" {
			marks[repo.Name] = ProjectMark{Name: repo.Project, Hue: repo.Hue}
		}
	}
	return marks
}

func refusedRepos(repos []store.Repo) []RefusedRepo {
	var refused []RefusedRepo
	for _, repo := range repos {
		if repo.Phase == plan.RepoRefused {
			refused = append(refused, RefusedRepo{Name: repo.Name, Reason: repo.Refusal, Path: RepoPath(repo.Name)})
		}
	}
	return refused
}

func (r *Reader) buildChrome(f frame) Chrome {
	params, now := f.params, f.now
	obs, observed, lastErr, gauges, split := f.in.Obs, f.in.Observed, f.lastErr, f.gauges, f.split
	c := Chrome{
		Workspace:    workspaceName(r.dataDir),
		LiveAgents:   liveAgents(f.tickets, obs),
		Observe:      Age{Age: "never"},
		ObserveStale: true,
		Gauges:       deriveGauges(gauges, split),
		PageTitle:    appTitle,
		Keymap:       Keymap,
		RepoScope:    params.Repo,
		RepoCrumb:    params.Repo,
		FeatureScope: params.Feature,
		Launch:       NavItem{Path: params.withScope("/launch"), Key: keyLaunch},
		Nav: []NavItem{
			{Label: "All tickets", Path: params.withScope(ticketsPath), Key: keyTickets, Icon: "inbox", Badge: "0"},
			{
				Label: "Insights", Key: keyInsights, Icon: "chart",
				Path: withQuery("/insights", Params{Feature: params.Feature, Project: params.Project}.Query()),
			},
			{Label: "Repos", Path: params.withScope("/repos"), Key: keyRepos, Icon: "repo"},
		},
	}
	c.setCurrent(keyTickets)
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
