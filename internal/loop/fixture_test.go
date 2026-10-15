package loop_test

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cctest"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/gittest"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
)

var testAt = time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
var discardLog = slog.New(slog.DiscardHandler)

func branchRef(repo, branch string) plan.BranchRef { return plan.BranchRef{Repo: repo, Branch: branch} }

func newLoop(
	st *storepkg.Store, observe loop.ObserveFunc, clock loop.Clock, cfg config.Config, ws config.Workspace,
	rn runner.Runner, opts ...loop.Option,
) *loop.Loop {
	return newLoopWithDeps(loop.Deps{
		Store: st, Observe: observe, Clock: clock, Config: cfg, Workspace: ws, Runner: rn,
	}, opts...)
}

func newLoopWithDeps(deps loop.Deps, opts ...loop.Option) *loop.Loop {
	deps.Logger = discardLog
	return loop.New(deps, opts...)
}

type fixture struct {
	Store                  *storepkg.Store
	Loop                   *loop.Loop
	Fake                   *runner.Fake
	Forge                  *recordingForge
	Worktrees              *recordingWorktrees
	Cfg                    config.Config
	WS                     config.Workspace
	Root                   string
	Tickets                []storepkg.Ticket
	Repo, Origin, Worktree string
	Ticket                 storepkg.Ticket
	Obs                    *plan.Observation
	MaxAgents              int
	SpendLimit5h           int
	ExtraRepo              string
}

func sandboxTicket(n string) storepkg.Ticket {
	return storepkg.Ticket{URL: "sandbox://CC-" + n, Repo: "repo", Branch: "cc-" + n}
}

func newLoopFixture(t *testing.T, f fixture) *fixture {
	t.Helper()
	if f.Tickets == nil {
		f.Tickets = []storepkg.Ticket{sandboxTicket("1")}
	}
	if f.MaxAgents == 0 {
		f.MaxAgents = 1
	}

	root, _ := repoWithOrigin(t)

	cfg, ws := testConfigAndWorkspace(t, root, f.MaxAgents, []string{"true"})
	cfg.SpendLimit5h = f.SpendLimit5h
	if f.ExtraRepo != "" {
		if err := os.Symlink(config.CheckoutPath(root, "repo"), config.CheckoutPath(root, f.ExtraRepo)); err != nil {
			t.Fatalf("Symlink(%q): %v", f.ExtraRepo, err)
		}
	}
	store := openStore(t)
	if err := store.UpsertTickets(t.Context(), f.Tickets); err != nil {
		t.Fatalf("UpsertTickets(%d tickets): %v", len(f.Tickets), err)
	}
	fake := runner.NewFake()
	forge := &recordingForge{}
	worktrees := &recordingWorktrees{}
	f.Store, f.Fake, f.Forge, f.Worktrees = store, fake, forge, worktrees
	f.Cfg, f.WS, f.Root = cfg, ws, root
	f.Loop = newLoop(store, noOpObserve, cctest.Clock{At: testAt}, cfg, ws, fake,
		loop.WithForge(forge), loop.WithWorktrees(worktrees))
	return &f
}

func (f *fixture) resetLaunch(rn runner.Runner, opts ...loop.Option) {
	f.Loop = newLoop(f.Store, noOpObserve, cctest.Clock{At: testAt}, f.Cfg, f.WS, rn,
		append([]loop.Option{loop.WithForge(f.Forge), loop.WithWorktrees(f.Worktrees)}, opts...)...)
}

func (f *fixture) AuthoriseAll(t *testing.T) {
	t.Helper()
	for _, ticket := range f.Tickets {
		authoriseTicket(t, f.Store, ticket.URL, composeHash(ticket.URL), testAt)
	}
}

func (f *fixture) Tick(t *testing.T) {
	t.Helper()
	if err := f.Loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
}

func (f *fixture) Latest(t *testing.T) map[string]plan.RunSummary {
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

func newPushFixture(t *testing.T, opts ...loop.Option) *fixture {
	t.Helper()
	root, repo := repoWithOrigin(t)
	worktree := cutWorktree(t, repo, "cc-1")
	st := openStore(t)
	ticket := sandboxTicket("1")
	storetest.Tickets(t, st, ticket)
	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	f := &fixture{
		Repo: repo, Origin: filepath.Join(root, "remote.git"), Worktree: worktree,
		Store: st, Ticket: ticket, Forge: &recordingForge{}, Root: root, Cfg: cfg, WS: ws,
	}
	obs := plan.NewObservation(testAt)
	obs.Worktrees[branchRef("repo", "cc-1")] = worktree
	f.Obs = &obs
	f.reset(testAt, runner.ProcessRunner{}, opts...)
	return f
}

func (f *fixture) reset(at time.Time, rn runner.Runner, opts ...loop.Option) {
	observe := func(context.Context) (plan.Observation, error) { return *f.Obs, nil }
	options := append([]loop.Option{loop.WithForge(f.Forge)}, opts...)
	f.Loop = newLoop(f.Store, observe, cctest.Clock{At: at}, f.Cfg, f.WS, rn, options...)
}

func (f *fixture) events(t *testing.T, kind storepkg.EventKind) []storepkg.Event {
	t.Helper()
	all, err := f.Store.Events(t.Context())
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	var selected []storepkg.Event
	for _, event := range all {
		if event.Kind == kind {
			selected = append(selected, event)
		}
	}
	return selected
}

func (f *fixture) head(t *testing.T, dir, ref string) string {
	t.Helper()
	return gittest.Run(t, dir, "rev-parse", ref)
}
