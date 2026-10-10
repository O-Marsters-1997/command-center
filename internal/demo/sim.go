package demo

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/config"
	ccgit "github.com/O-Marsters-1997/command-center/internal/git"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/tracker"
	"github.com/O-Marsters-1997/command-center/internal/web"
)

const maxAgents = 4

var simStart = time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)

var glyphRE = regexp.MustCompile(`<span class="glyph[^"]*">([^<]*)</span>`)

type issue struct {
	Ticket
	repo   *sandboxRepo
	number int
	url    string
	branch string
}

func (i issue) tracker(urlByID map[string]string) tracker.Ticket {
	blockedBy := make([]string, 0, len(i.BlockedBy))
	for _, id := range i.BlockedBy {
		blockedBy = append(blockedBy, urlByID[id])
	}
	return tracker.Ticket{URL: i.url, Number: i.number, Title: i.Title, BlockedBy: blockedBy}
}

func buildIssues(sc Scenario, sb *Sandbox) ([]issue, error) {
	repos := map[string]*sandboxRepo{}
	for _, r := range sb.repos {
		repos[r.scenarioName] = r
	}
	issues := make([]issue, 0, len(sc.Tickets))
	for n, t := range sc.Tickets {
		repo, ok := repos[t.Repo]
		if !ok {
			return nil, fmt.Errorf("ticket %q: no sandbox repo %q", t.ID, t.Repo)
		}
		number := n + 1
		issues = append(issues, issue{
			Ticket: t,
			repo:   repo,
			number: number,
			url:    fmt.Sprintf("https://github.com/%s/issues/%d", t.Repo, number),
			branch: tracker.BranchSlug(number, t.Title),
		})
	}
	return issues, nil
}

// SimClock is a loop.Clock that only moves when Advance is called, so a run plays without
// sleeping.
type SimClock struct {
	mu      sync.Mutex
	now     time.Time
	waiters []waiter
}

type waiter struct {
	at time.Time
	ch chan time.Time
}

func NewSimClock(start time.Time) *SimClock { return &SimClock{now: start} }

func (c *SimClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *SimClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	if d <= 0 {
		ch <- c.now
		return ch
	}
	c.waiters = append(c.waiters, waiter{at: c.now.Add(d), ch: ch})
	return ch
}

func (c *SimClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	pending := c.waiters[:0]
	for _, w := range c.waiters {
		if w.at.After(c.now) {
			pending = append(pending, w)
			continue
		}
		w.ch <- c.now
	}
	c.waiters = pending
}

// Sim plays a Scenario: the real loop and board over real git, against the fake forge, tracker
// and agent. It only changes the world the loop observes and presses launch as a human would.
type Sim struct {
	scenario Scenario
	clock    *SimClock
	sandbox  *Sandbox
	forge    *Forge
	agent    *Agent
	store    *store.Store
	loop     *loop.Loop
	server   *web.Server
	issues   []issue

	authorised map[string]bool
	board      map[string]string
	landed     int
}

func NewSim(ctx context.Context, sc Scenario) (_ *Sim, err error) {
	sb, err := NewSandbox(sc.Repos)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, sb.Close())
		}
	}()

	issues, err := buildIssues(sc, sb)
	if err != nil {
		return nil, err
	}
	clock := NewSimClock(simStart)
	forge := NewForge(clock, sb, issues)
	agent := NewAgent(clock, issues, sc.Seed)
	resolve := trackerSource(issues)

	ws, err := workspaceIn(sb)
	if err != nil {
		return nil, err
	}
	st, err := store.OpenStore(sb.DSN)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, st.Close())
		}
	}()

	cfg := config.Config{DataDir: ws.DataDir, MaxAgents: maxAgents, AgentCommand: []string{"demo-agent"}}
	for _, repo := range sb.Repos(clock.Now()) {
		if err := ccgit.EnsureCheckout(ctx, repo.Name, repo.Remote, config.CheckoutPath(cfg.DataDir, repo.Name)); err != nil {
			return nil, err
		}
		if err := st.UpsertRepo(ctx, repo); err != nil {
			return nil, err
		}
	}

	lp := loop.NewLoop(st, loop.NewObserver(st, forge, cfg), clock, cfg, ws, agent)
	lp.SetForge(forge)
	lp.SetWorktrees(Worktrees{})
	lp.SetTrackerSource(resolve)
	server := web.NewServer(st, clock, ws.DataDir)
	server.SetTrackerSource(resolve)
	server.SetBoardPollSeconds(1)
	server.AllowAnonymous()

	s := &Sim{
		scenario: sc, clock: clock, sandbox: sb, forge: forge, agent: agent, store: st,
		loop: lp, server: server, issues: issues,
		authorised: map[string]bool{}, board: map[string]string{},
	}
	for _, feature := range features(issues) {
		if err := st.QueueVerbIntent(ctx, feature, store.ImportVerb, clock.Now()); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func features(issues []issue) []string {
	var out []string
	for _, i := range issues {
		if !slices.Contains(out, i.Feature) {
			out = append(out, i.Feature)
		}
	}
	return out
}

func workspaceIn(sb *Sandbox) (config.Workspace, error) {
	state := filepath.Join(sb.root, "state")
	ws := config.Workspace{
		DataDir:          sb.root,
		StateDir:         state,
		RunsDir:          sb.RunsDir(),
		SettingsPath:     filepath.Join(state, "agent.json"),
		SystemPromptPath: filepath.Join(state, "system-prompt.md"),
		AgentsPath:       filepath.Join(state, "agents.json"),
	}
	if err := os.MkdirAll(ws.RunsDir, 0o750); err != nil {
		return ws, err
	}
	if err := os.MkdirAll(state, 0o750); err != nil {
		return ws, err
	}
	return ws, loop.WriteAgentFiles(ws)
}

// Tick plays one loop tick: the world moves, the loop runs, then the sim reads every board state
// and presses launch for tickets that reached ready.
func (s *Sim) Tick(ctx context.Context) error {
	if err := s.landMain(); err != nil {
		return err
	}
	if err := s.forge.Advance(); err != nil {
		return err
	}
	if err := s.agent.Step(); err != nil {
		return err
	}
	if err := s.loop.RunOnce(ctx); err != nil {
		return err
	}
	states, err := s.states()
	if err != nil {
		return err
	}
	s.board = states
	if err := s.launchReady(states); err != nil {
		return err
	}
	s.clock.Advance(store.TickPeriod)
	return nil
}

// Serve runs the sim at speed sim seconds per real second and serves its board on addr until ctx
// is cancelled.
func Serve(ctx context.Context, sc Scenario, speed float64, addr string) error {
	sim, err := NewSim(ctx, sc)
	if err != nil {
		return err
	}
	defer func() {
		if err := sim.Close(); err != nil {
			log.Printf("demo: close: %v", err)
		}
	}()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	srv := &http.Server{
		Addr:              addr,
		Handler:           sim.server,
		ReadHeaderTimeout: 5 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	errs := make(chan error, 2)
	go func() { errs <- sim.run(ctx, speed) }()
	go func() {
		err := srv.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errs <- err
	}()

	first := <-errs
	cancel()
	shutdown, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer stop()
	return errors.Join(first, srv.Shutdown(shutdown), <-errs)
}

func (s *Sim) run(ctx context.Context, speed float64) error {
	wait := time.Duration(float64(store.TickPeriod) / speed)
	for ctx.Err() == nil {
		if err := s.Tick(ctx); err != nil && ctx.Err() == nil {
			return err
		}
		select {
		case <-ctx.Done():
		case <-time.After(wait):
		}
	}
	return nil
}

func (s *Sim) Close() error { return errors.Join(s.store.Close(), s.sandbox.Close()) }

func (s *Sim) states() (map[string]string, error) {
	rec := httptest.NewRecorder()
	s.server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/board", nil))
	if rec.Code != http.StatusOK {
		return nil, fmt.Errorf("GET /board: %s", rec.Body)
	}
	rows := strings.Split(rec.Body.String(), "<tr")
	states := make(map[string]string, len(s.issues))
	for _, i := range s.issues {
		ref := fmt.Sprintf(">#%s</button>", path.Base(i.url))
		for _, row := range rows {
			if !strings.Contains(row, ref) {
				continue
			}
			if glyph := glyphRE.FindStringSubmatch(row); glyph != nil {
				states[i.ID] = glyph[1]
			}
		}
	}
	return states, nil
}

func (s *Sim) launchReady(states map[string]string) error {
	form := url.Values{}
	for _, i := range s.issues {
		if s.authorised[i.ID] || states[i.ID] != "ready" {
			continue
		}
		s.authorised[i.ID] = true
		form.Add("ticket", i.url)
	}
	if len(form) == 0 {
		return nil
	}
	req := httptest.NewRequest(http.MethodPost, "/launch", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.server.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		return fmt.Errorf("POST /launch: %d: %s", rec.Code, rec.Body)
	}
	return nil
}

func (s *Sim) landMain() error {
	elapsed := s.clock.Now().Sub(simStart)
	for ; s.landed < len(s.scenario.Main); s.landed++ {
		m := s.scenario.Main[s.landed]
		if m.At > elapsed {
			return nil
		}
		if err := s.sandbox.LandOnMain(m.Repo, m.Files); err != nil {
			return err
		}
	}
	return nil
}
