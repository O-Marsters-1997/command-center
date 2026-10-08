package loop_test

import (
	"os"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

var testAt = time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

type loopFixture struct {
	Store   *storepkg.Store
	Loop    *loop.Loop
	Fake    *runner.Fake
	Cfg     config.Config
	WS      config.Workspace
	Root    string
	Tickets []storepkg.Ticket
}

type fixtureConfig struct {
	maxAgents  int
	spendLimit int
	tpFails    bool
	tickets    []storepkg.Ticket
	extraRepo  string
}

type fixtureOption func(*fixtureConfig)

func withMaxAgents(n int) fixtureOption { return func(c *fixtureConfig) { c.maxAgents = n } }

func withSpendLimit5h(limit int) fixtureOption {
	return func(c *fixtureConfig) { c.spendLimit = limit }
}

func withFailingTp() fixtureOption { return func(c *fixtureConfig) { c.tpFails = true } }

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
	installFakeTp(t, fc.tpFails)
	installFakeGh(t, false)

	cfg, ws := testConfigAndWorkspace(t, root, fc.maxAgents, []string{"true"})
	cfg.SpendLimit5h = fc.spendLimit
	if fc.extraRepo != "" {
		if err := os.Symlink(config.CheckoutPath(root, "repo"), config.CheckoutPath(root, fc.extraRepo)); err != nil {
			t.Fatal(err)
		}
	}
	store := openStore(t)
	if err := store.UpsertTickets(t.Context(), fc.tickets); err != nil {
		t.Fatal(err)
	}
	fake := runner.NewFake()
	return &loopFixture{
		Store:   store,
		Loop:    loop.NewLoop(store, noOpObserve, fixedClock(testAt), cfg, ws, fake),
		Fake:    fake,
		Cfg:     cfg,
		WS:      ws,
		Root:    root,
		Tickets: fc.tickets,
	}
}

func (f *loopFixture) AuthoriseAll(t *testing.T) {
	t.Helper()
	for _, ticket := range f.Tickets {
		authoriseTicket(t, f.Store, ticket.URL, plan.Hash(plan.Compose(plan.Ticket{URL: ticket.URL})), testAt)
	}
}

func (f *loopFixture) Tick(t *testing.T) {
	t.Helper()
	if err := f.Loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
}

func (f *loopFixture) Latest(t *testing.T) map[string]plan.RunSummary {
	t.Helper()
	latest, err := f.Store.LatestRunsByTicket(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return latest
}
