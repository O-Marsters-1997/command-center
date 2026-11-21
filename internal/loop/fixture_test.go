package loop_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/adapter/gh"
	"github.com/O-Marsters-1997/command-center/internal/adapter/gh/ghtest"
	"github.com/O-Marsters-1997/command-center/internal/adapter/git"
	"github.com/O-Marsters-1997/command-center/internal/adapter/git/gittest"
	"github.com/O-Marsters-1997/command-center/internal/adapter/runner"
	"github.com/O-Marsters-1997/command-center/internal/adapter/runner/runnertest"
	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
)

var testAt = time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

func openPRs(t *testing.T, forge *ghtest.Forge) map[string]gh.PR {
	t.Helper()
	snap, err := forge.List(t.Context(), "/repo", nil)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	return snap.ByBranch
}

func newLoop(
	st *storepkg.Store, observe loop.ObserveFunc, clock loop.Clock, cfg config.Config, ws config.Workspace,
	rn runner.Runner, opts ...loop.Option,
) *loop.Loop {
	return newLoopWithDeps(loop.Deps{
		Store: st, Observe: observe, Clock: clock, Config: cfg, Workspace: ws, Runner: rn,
	}, opts...)
}

func newLoopWithDeps(deps loop.Deps, opts ...loop.Option) *loop.Loop {
	observe := deps.Observe
	completeObserve := func(ctx context.Context) (plan.Observation, error) {
		obs, err := observe(ctx)
		if err != nil {
			return obs, err
		}
		if obs.LocalTips == nil {
			obs.LocalTips = map[plan.BranchRef]plan.SHA{}
		}
		if obs.Runs == nil {
			obs.Runs = map[plan.TicketURL]plan.RunObservation{}
		}
		return obs, nil
	}
	if deps.Logger == nil {
		deps.Logger = discardLog
	}
	deps.Observe = completeObserve
	return loop.New(deps, append([]loop.Option{loop.WithForge(&ghtest.Forge{})}, opts...)...)
}

type loopFixture struct {
	Store   *storepkg.Store
	Loop    *loop.Loop
	Fake    *runnertest.Runner
	Forge   *ghtest.Forge
	Cfg     config.Config
	WS      config.Workspace
	Root    string
	Tickets []storepkg.Ticket
}

type fixtureConfig struct {
	maxAgents  int
	spendLimit int
	cutFails   bool
	tickets    []storepkg.Ticket
	extraRepo  string
}

type fixtureOption func(*fixtureConfig)

func withMaxAgents(n int) fixtureOption { return func(c *fixtureConfig) { c.maxAgents = n } }

func withSpendLimit5h(limit int) fixtureOption {
	return func(c *fixtureConfig) { c.spendLimit = limit }
}

func withFailingCut() fixtureOption { return func(c *fixtureConfig) { c.cutFails = true } }

func withExtraRepo(name string) fixtureOption { return func(c *fixtureConfig) { c.extraRepo = name } }

func withTickets(tickets ...storepkg.Ticket) fixtureOption {
	return func(c *fixtureConfig) { c.tickets = tickets }
}

func sandboxTicket(n string) storepkg.Ticket {
	return storepkg.Ticket{URL: "sandbox://CC-" + n, Repo: "repo", Branch: "cc-" + n}
}

func newLoopFixture(t *testing.T, opts ...fixtureOption) *loopFixture {
	t.Helper()
	fc := fixtureConfig{maxAgents: 1, tickets: []storepkg.Ticket{sandboxTicket("1")}}
	for _, opt := range opts {
		opt(&fc)
	}

	root, _ := repoWithOrigin(t)

	cfg, ws := testConfigAndWorkspace(t, root, fc.maxAgents, []string{"true"})
	cfg.SpendLimit5h = fc.spendLimit
	if fc.extraRepo != "" {
		if err := os.Symlink(config.CheckoutPath(root, "repo"), config.CheckoutPath(root, fc.extraRepo)); err != nil {
			t.Fatalf("Symlink(%q): %v", fc.extraRepo, err)
		}
	}
	store := openStore(t)
	if err := store.UpsertTickets(t.Context(), fc.tickets); err != nil {
		t.Fatalf("UpsertTickets(%d tickets): %v", len(fc.tickets), err)
	}
	fake := &runnertest.Runner{}
	forge := &ghtest.Forge{}
	cut := loop.WithWorktrees(git.Library{})
	if fc.cutFails {
		cut = loop.WithWorktrees(failingCut{})
	}
	return &loopFixture{
		Store:   store,
		Loop:    newLoop(store, noOpObserve, fixedClock(testAt), cfg, ws, fake, loop.WithForge(forge), cut),
		Fake:    fake,
		Forge:   forge,
		Cfg:     cfg,
		WS:      ws,
		Root:    root,
		Tickets: fc.tickets,
	}
}

func (f *loopFixture) AuthoriseAll(t *testing.T) {
	t.Helper()
	for _, ticket := range f.Tickets {
		authoriseTicket(t, f.Store, ticket.URL, composeHash(ticket.URL), testAt)
	}
}

func (f *loopFixture) Tick(t *testing.T) {
	t.Helper()
	runOnce(t, f.Loop)
}

func (f *loopFixture) Latest(t *testing.T) map[string]plan.RunSummary {
	t.Helper()
	latest, err := f.Store.LatestRunsByTicket(t.Context())
	if err != nil {
		t.Fatalf("LatestRunsByTicket(): %v", err)
	}
	byURL := make(map[string]plan.RunSummary, len(latest))
	for url, summary := range latest {
		byURL[string(url)] = summary
	}
	return byURL
}

func composeHash(ticketURL string) string {
	return plan.Hash(plan.Compose(plan.Ticket{URL: plan.TicketURL(ticketURL)}))
}

type pushFixture struct {
	Repo, Origin, Worktree string
	Store                  *storepkg.Store
	Ticket                 storepkg.Ticket
	Obs                    *plan.Observation
	Forge                  *ghtest.Forge
	Loop                   *loop.Loop
	root                   string
	cfg                    config.Config
	ws                     config.Workspace
}

func newPushFixture(t *testing.T, opts ...loop.Option) *pushFixture {
	t.Helper()
	root, repo := repoWithOrigin(t)
	worktree := cutWorktree(t, repo, "cc-1")
	st := openStore(t)
	ticket := sandboxTicket("1")
	storetest.Tickets(t, st, ticket)
	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	forge := &ghtest.Forge{}
	forge.Seed(ghtest.Seed{Checkouts: map[string]string{worktree: "cc-1"}})
	f := &pushFixture{
		Repo: repo, Origin: filepath.Join(root, "remote.git"), Worktree: worktree,
		Store: st, Ticket: ticket, Forge: forge, root: root, cfg: cfg, ws: ws,
		Obs: &plan.Observation{Worktrees: map[plan.BranchRef]string{branchRef("repo", "cc-1"): worktree},
			PRs: map[plan.BranchRef]plan.PR{}},
	}
	f.reset(testAt, runner.ProcessRunner{}, opts...)
	return f
}

func (f *pushFixture) reset(at time.Time, rn runner.Runner, opts ...loop.Option) {
	observe := func(context.Context) (plan.Observation, error) { return *f.Obs, nil }
	options := append([]loop.Option{loop.WithForge(f.Forge)}, opts...)
	f.Loop = newLoop(f.Store, observe, fixedClock(at), f.cfg, f.ws, rn, options...)
}

func (f *pushFixture) tick(t *testing.T) {
	t.Helper()
	runOnce(t, f.Loop)
}

func (f *pushFixture) head(t *testing.T, dir, ref string) string {
	t.Helper()
	return gittest.Run(t, dir, "rev-parse", ref)
}
