package view

import (
	"context"
	"errors"
	"net/url"
	"path/filepath"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/spend"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

const observeStaleAfter = 20 * time.Second

// Board is the board page's view model: the grouped rows, the band over them and the chrome
// every page wears.
type Board struct {
	Chrome
	Groups           []Group
	Band             Band
	BoardPath        string
	BoardPollSeconds int
}

func (b Board) Row(ticketURL string) (Row, bool) {
	for _, g := range b.Groups {
		if g.Root != nil && g.Root.URL == ticketURL {
			return *g.Root, true
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
	rules            plan.Rules
	repos            []config.Repo
	dataDir          string
	spendLimit5h     int
	boardPollSeconds int
	spend            *SpendCache
	renderLine       LineRenderer
}

func NewReader(st *store.Store, repos []config.Repo, dataDir string, renderLine LineRenderer) *Reader {
	return &Reader{
		store: st, repos: repos, dataDir: dataDir, renderLine: renderLine,
		rules:            config.Config{Repos: repos}.PlanRules(),
		boardPollSeconds: config.DefaultBoardPollSeconds,
		spend:            NewSpendCache(),
	}
}

func (r *Reader) SetBoardPollSeconds(seconds int) { r.boardPollSeconds = seconds }

func (r *Reader) SetSpendLimit5h(pct int) { r.spendLimit5h = pct }

func (r *Reader) Snapshot(ctx context.Context, now time.Time) (plan.Snapshot, error) {
	in, err := r.store.PlanInput(ctx)
	if err != nil {
		return plan.Snapshot{}, err
	}
	in.Now = now
	return r.rules.Derive(in), nil
}

// Board derives the board page. The repo and feature scopes narrow the groups after grouping, so
// a group with a member in scope stays whole.
func (r *Reader) Board(ctx context.Context, now time.Time, params Params) (Board, error) {
	params.Repo = normalizeRepoScope(params.Repo, r.rules.Stacking)

	tickets, err := r.store.Tickets(ctx)
	if err != nil {
		return Board{}, err
	}
	params.Feature = normalizeFeatureScope(params.Feature, distinctFeatures(tickets))
	in, err := r.store.PlanInput(ctx)
	if err != nil {
		return Board{}, err
	}
	lastErr, failed, err := r.store.LastError(ctx)
	if err != nil {
		return Board{}, err
	}
	gauges, err := r.store.LatestReadings(ctx)
	if err != nil {
		return Board{}, err
	}

	in.Now = now
	split, err := r.gaugeSplit(ctx, now)
	if err != nil {
		return Board{}, err
	}
	ticketSpend, err := r.store.BoardTicketSpend(ctx, params.Repo, params.Feature)
	if err != nil {
		return Board{}, err
	}

	rows := deriveRows(tickets, in, r.rules.Derive(in))
	applySpend(rows, r.spend)
	applyTicketSpend(rows, ticketSpend, split[agentlog.SevenDay].Fit.Factor)
	applyViewState(rows, params, r.renderLine)
	if err := r.applyContextCurve(ctx, rows); err != nil {
		return Board{}, err
	}
	groups := filterGroupsByFeature(filterGroupsByRepo(groupRows(rows), params.Repo), params.Feature)
	return Board{
		Chrome:           r.buildChrome(tickets, in.Obs, in.Observed, lastErr, failed, gauges, split, now, params),
		Groups:           groups,
		Band:             deriveBand(rowsIn(groups)),
		BoardPath:        params.boardPath(),
		BoardPollSeconds: r.boardPollSeconds,
	}, nil
}

func (r *Reader) applyContextCurve(ctx context.Context, rows []Row) error {
	for i := range rows {
		if !rows[i].Selected || rows[i].RunID == 0 {
			continue
		}
		requests, err := r.store.RunRequestsForRun(ctx, rows[i].RunID)
		if err != nil {
			return err
		}
		rows[i].ContextCurve = buildContextCurve(requests)
	}
	return nil
}

// Chrome builds a page's chrome without Board's derivation, groups or verdicts: the one cheap
// read /features, /insights and /confirm make for the workspace, live and observe pills, and
// scope links that every page's topbar and masthead show.
func (r *Reader) Chrome(ctx context.Context, now time.Time, params Params) (Chrome, error) {
	tickets, err := r.store.Tickets(ctx)
	if err != nil {
		return Chrome{}, err
	}
	params.Repo = normalizeRepoScope(params.Repo, r.rules.Stacking)
	params.Feature = normalizeFeatureScope(params.Feature, distinctFeatures(tickets))

	obs, observed, err := r.store.LastObservation(ctx)
	if err != nil {
		return Chrome{}, err
	}
	lastErr, failed, err := r.store.LastError(ctx)
	if err != nil {
		return Chrome{}, err
	}
	gauges, err := r.store.LatestReadings(ctx)
	if err != nil {
		return Chrome{}, err
	}
	split, err := r.gaugeSplit(ctx, now)
	if err != nil {
		return Chrome{}, err
	}
	return r.buildChrome(tickets, obs, observed, lastErr, failed, gauges, split, now, params), nil
}

func (r *Reader) buildChrome(
	tickets []store.Ticket, obs plan.Observation, observed bool, lastErr store.TickError, failed bool,
	gauges map[agentlog.Window]store.Gauge, split map[agentlog.Window]windowSplit, now time.Time, params Params,
) Chrome {
	c := Chrome{
		Workspace:    workspaceName(r.dataDir),
		LiveAgents:   liveAgents(tickets, obs),
		Observe:      Age{Age: "never"},
		ObserveStale: true,
		Gauges:       deriveGauges(gauges, split),
		View:         params.View,
		Section:      params.View,
		RepoScope:    params.Repo,
		RepoLinks:    repoLinksFor(r.repos, params),
		FeatureScope: params.Feature,
	}
	if fiveHour := gauges[agentlog.FiveHour].Utilization; spend.Paused(fiveHour, r.spendLimit5h) {
		c.SpendPaused = &SpendPaused{Pct: spend.Pct(fiveHour), Limit: r.spendLimit5h}
	}
	if observed {
		c.Observe = relative(now, obs.ObservedAt)
		c.ObserveStale = now.Sub(obs.ObservedAt) >= observeStaleAfter
	}
	if failed && (!observed || lastErr.At.After(obs.ObservedAt)) {
		c.LastError = &TickError{Age: relative(now, lastErr.At), Message: lastErr.Message}
	}
	if params.Feature != "" {
		c.FeatureImportPath = params.featureImportPath()
		c.FeatureQuery = url.QueryEscape(params.Feature)
	}
	return c
}

func (r *Reader) gaugeSplit(ctx context.Context, now time.Time) (map[agentlog.Window]windowSplit, error) {
	fits, err := r.store.FitFactors(ctx, now)
	if err != nil {
		return nil, err
	}
	ccCost, err := r.store.CCCostUSD(ctx, now)
	if err != nil {
		return nil, err
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
		if obs.Runs[t.URL].Alive {
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
