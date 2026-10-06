package demo

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/cc"
	"github.com/O-Marsters-1997/command-center/internal/config"
	ccgit "github.com/O-Marsters-1997/command-center/internal/git"
	"github.com/O-Marsters-1997/command-center/internal/verdict"
)

const tickPeriod = 15 * time.Second

var simStart = time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)

var pillRE = regexp.MustCompile(`<span class="pill[^"]*">([^<]*)</span>`)

// Transition is one ticket's board state changing, at a sim time since the run started.
type Transition struct {
	At     time.Duration
	Ticket string
	State  string
}

// Sim plays a Scenario: the real loop and board over real git, against the fake forge, tracker
// and agent. It only changes the world the loop observes and presses the buttons a human would.
type Sim struct {
	scenario Scenario
	clock    *SimClock
	sandbox  *Sandbox
	forge    *Forge
	agent    *Agent
	store    *cc.Store
	loop     *cc.Loop
	server   *cc.Server
	issues   []issue

	authorised  map[string]bool
	last        map[string]string
	transitions []Transition
	checked     int
	landed      int
	pushed      int
	pressed     int
	mismatches  []error
}

// NewSim builds the sandbox and the loop for sc and queues the import of every ticket's feature.
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

	verifyCommand, err := installFailureScripts(sb, issues)
	if err != nil {
		return nil, err
	}
	template := config.Repo{Tracker: "github", Checks: verdict.Predicate{Success: ciCheck}, VerifyCommand: verifyCommand}
	cfg := config.Config{
		MaxAgents:    len(issues),
		AgentCommand: []string{"demo-agent"},
		Repos:        sb.Repos(template),
	}
	for _, repo := range cfg.Repos {
		if err := ccgit.EnsureCheckout(ctx, repo.Name, repo.Remote, repo.Checkout); err != nil {
			return nil, err
		}
	}
	ws, err := workspaceIn(sb)
	if err != nil {
		return nil, err
	}
	store, err := cc.OpenStore(sb.DSN)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, store.Close())
		}
	}()

	loop := cc.NewLoop(store, cc.NewObserver(store, forge, cfg), clock, cfg, ws, agent)
	loop.SetMetricsParser(agentlog.ParseMetrics)
	loop.SetForge(forge)
	loop.SetWorktrees(NewWorktrees(issues))
	loop.SetTrackerSource(resolve)
	server := cc.NewServer(store, clock, cfg.Repos, ws.DataDir)
	server.SetTrackerSource(resolve)
	server.SetBoardPollSeconds(1)

	s := &Sim{
		scenario: sc, clock: clock, sandbox: sb, forge: forge, agent: agent, store: store,
		loop: loop, server: server, issues: issues,
		authorised: map[string]bool{}, last: map[string]string{},
	}
	for _, feature := range features(issues) {
		if err := cc.QueueImport(ctx, store, feature, clock.Now()); err != nil {
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
	return ws, errors.Join(
		cc.WriteAgentSettings(ws.SettingsPath),
		cc.WriteAgentSystemPrompt(ws.SystemPromptPath),
		cc.WriteAgentDigestDefinition(ws.AgentsPath),
	)
}

// Elapsed is the sim time since the run started.
func (s *Sim) Elapsed() time.Duration { return s.clock.Now().Sub(simStart) }

// Tick plays one loop tick: the world moves, the loop runs, then the sim records every board
// state, presses launch for tickets that reached ready, and checks the checkpoints now due.
func (s *Sim) Tick(ctx context.Context) error {
	if err := s.landMain(); err != nil {
		return err
	}
	if err := s.landPushes(); err != nil {
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
	s.record(states)
	if err := s.launchReady(states); err != nil {
		return err
	}
	if err := s.pressDue(); err != nil {
		return err
	}
	s.check(states)
	s.clock.Advance(tickPeriod)
	return nil
}

// Play ticks until every checkpoint has been checked, and returns the mismatches.
func (s *Sim) Play(ctx context.Context) error {
	for s.checked < len(s.scenario.Expect) {
		if err := s.Tick(ctx); err != nil {
			return err
		}
	}
	return errors.Join(s.mismatches...)
}

// PlayTo ticks until the sim has run to at, without checking any checkpoint.
func (s *Sim) PlayTo(ctx context.Context, at time.Duration) error {
	for s.Elapsed() <= at {
		if err := s.Tick(ctx); err != nil {
			return err
		}
	}
	return nil
}

// Board is each ticket's state as of the last tick, by scenario ticket id.
func (s *Sim) Board() map[string]string { return maps.Clone(s.last) }

// Transitions is every state change so far, in order.
func (s *Sim) Transitions() []Transition { return slices.Clone(s.transitions) }

// Close tears down the store and the sandbox.
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
			if pill := pillRE.FindStringSubmatch(row); pill != nil {
				states[i.ID] = pill[1]
			}
		}
	}
	return states, nil
}

func (s *Sim) record(states map[string]string) {
	for _, i := range s.issues {
		state, ok := states[i.ID]
		if !ok || state == s.last[i.ID] {
			continue
		}
		s.last[i.ID] = state
		s.transitions = append(s.transitions, Transition{At: s.Elapsed(), Ticket: i.ID, State: state})
	}
}

func (s *Sim) launchReady(states map[string]string) error {
	var ready []string
	for _, i := range s.issues {
		if s.authorised[i.ID] || !i.launchDue(states[i.ID]) {
			continue
		}
		s.authorised[i.ID] = true
		if i.Launch == launchEarly {
			if err := s.postLaunch([]string{i.url}); err != nil {
				return err
			}
			continue
		}
		ready = append(ready, i.url)
	}
	return s.postLaunch(ready)
}

func (s *Sim) postLaunch(tickets []string) error {
	if len(tickets) == 0 {
		return nil
	}
	form := url.Values{"ticket": tickets}
	req := httptest.NewRequest(http.MethodPost, "/launch", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.server.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		return fmt.Errorf("POST /launch: %d: %s", rec.Code, rec.Body)
	}
	return nil
}

func (i issue) launchDue(state string) bool {
	switch i.Launch {
	case launchHold:
		return false
	case launchEarly:
		return true
	default:
		return state == "ready"
	}
}

func (s *Sim) landMain() error {
	for ; s.landed < len(s.scenario.Main); s.landed++ {
		m := s.scenario.Main[s.landed]
		if time.Duration(m.At) > s.Elapsed() {
			return nil
		}
		if err := s.sandbox.LandOnMain(m.Repo, m.Files); err != nil {
			return err
		}
	}
	return nil
}

func (s *Sim) landPushes() error {
	for ; s.pushed < len(s.scenario.Push); s.pushed++ {
		p := s.scenario.Push[s.pushed]
		if time.Duration(p.At) > s.Elapsed() {
			return nil
		}
		owner := s.issueByID(p.Ticket)
		if err := s.sandbox.PushToBranch(owner.repo, owner.branch, p.Files); err != nil {
			return err
		}
	}
	return nil
}

func (s *Sim) pressDue() error {
	for ; s.pressed < len(s.scenario.Press); s.pressed++ {
		p := s.scenario.Press[s.pressed]
		if time.Duration(p.At) > s.Elapsed() {
			return nil
		}
		form := url.Values{"verb": {p.Verb}, "ticket": {s.issueByID(p.Ticket).url}}
		req := httptest.NewRequest(http.MethodPost, "/verb", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		s.server.ServeHTTP(rec, req)
		if rec.Code != http.StatusSeeOther {
			return fmt.Errorf("POST /verb %s %s: %d: %s", p.Verb, p.Ticket, rec.Code, rec.Body)
		}
	}
	return nil
}

func (s *Sim) issueByID(id string) issue {
	return s.issues[slices.IndexFunc(s.issues, func(i issue) bool { return i.ID == id })]
}

func (s *Sim) check(states map[string]string) {
	for ; s.checked < len(s.scenario.Expect); s.checked++ {
		e := s.scenario.Expect[s.checked]
		if time.Duration(e.At) > s.Elapsed() {
			return
		}
		if got := states[e.Ticket]; got != e.State {
			s.mismatches = append(s.mismatches,
				fmt.Errorf("at %s ticket %s is %q, want %q", s.Elapsed(), e.Ticket, got, e.State))
		}
	}
}

// RunLogs is the path of every agent log the loop has opened, in run order.
func (s *Sim) RunLogs() []string {
	logs, _ := filepath.Glob(filepath.Join(s.sandbox.RunsDir(), "*.jsonl"))
	return logs
}
