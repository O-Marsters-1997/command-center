package cc

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
)

// App is one Command Centre instance: the flock, the store, the loop and the page.
type App struct {
	cfg    Config
	lock   *Flock
	store  *Store
	loop   *Loop
	server *Server
}

type options struct {
	now           func() time.Time
	observe       ObserveFunc
	repoCheck     RepoCheckFunc
	checkout      CheckoutFunc
	runner        Runner
	metricsParser MetricsParser
}

// Option configures New.
type Option func(*options)

// WithClock replaces time.Now. Injecting it is what makes the rendered page byte-stable in
// tests; no test ever sleeps.
func WithClock(now func() time.Time) Option {
	return func(o *options) { o.now = now }
}

// WithObserver replaces the observe phase, so a tick can be driven without git or gh.
func WithObserver(observe ObserveFunc) Option {
	return func(o *options) { o.observe = observe }
}

// RepoCheckFunc asserts the configured repos' merge settings. See AssertReposSquashOnly.
type RepoCheckFunc func(ctx context.Context, ws Workspace, repos []Repo) error

// WithRepoCheck replaces the startup squash-only check, so a test can run without gh.
func WithRepoCheck(check RepoCheckFunc) Option {
	return func(o *options) { o.repoCheck = check }
}

// WithRunner replaces the real process runner, so a test can drive spawn, liveness and cancel
// without touching the OS.
func WithRunner(runner Runner) Option {
	return func(o *options) { o.runner = runner }
}

// CheckoutFunc ensures every configured repo has a working checkout before the loop starts. See
// EnsureCheckout.
type CheckoutFunc func(ctx context.Context, repos []Repo) error

// WithCheckout replaces the startup checkout step, so a test can substitute its own checkout
// preparation for a repo whose remote isn't really dialable.
func WithCheckout(checkout CheckoutFunc) Option {
	return func(o *options) { o.checkout = checkout }
}

// WithMetricsParser replaces the run-log metrics parser, so a test can substitute a fake without
// touching the filesystem.
func WithMetricsParser(p MetricsParser) Option {
	return func(o *options) { o.metricsParser = p }
}

func ensureAllCheckouts(ctx context.Context, repos []Repo) error {
	for _, repo := range repos {
		if err := EnsureCheckout(ctx, repo); err != nil {
			return err
		}
	}
	return nil
}

// New resolves the workspace, takes the flock and opens the store. A second instance against the
// same workspace is refused (inv. 9).
func New(ctx context.Context, configPath string, opts ...Option) (app *App, err error) {
	settings := options{now: time.Now}
	for _, opt := range opts {
		opt(&settings)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		return nil, err
	}
	ws, err := ResolveWorkspace(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	checkout := settings.checkout
	if checkout == nil {
		checkout = ensureAllCheckouts
	}
	if err := checkout(ctx, cfg.Repos); err != nil {
		return nil, err
	}

	repoCheck := settings.repoCheck
	if repoCheck == nil {
		repoCheck = AssertReposSquashOnly
	}
	if err := repoCheck(ctx, ws, cfg.Repos); err != nil {
		return nil, err
	}

	lock, err := Lock(ws.LockPath)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, lock.Close())
		}
	}()

	store, err := OpenStore(cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, store.Close())
		}
	}()

	// Written once at startup rather than per spawn: the content never varies, and every spawn
	// just passes the same path (inv. 17).
	if err := WriteAgentSettings(ws.SettingsPath); err != nil {
		return nil, err
	}
	if err := WriteAgentSystemPrompt(ws.SystemPromptPath); err != nil {
		return nil, err
	}

	observe := settings.observe
	if observe == nil {
		observe = NewObserver(store, cfg)
	}
	runner := settings.runner
	if runner == nil {
		runner = ProcessRunner{}
	}
	metricsParser := settings.metricsParser
	if metricsParser == nil {
		metricsParser = agentlog.ParseMetrics
	}
	if err := BackfillMetrics(ctx, store, metricsParser); err != nil {
		return nil, err
	}

	loop := NewLoop(store, observe, settings.now, cfg, ws, runner)
	loop.SetMetricsParser(metricsParser)
	server := NewServer(store, settings.now, cfg.Repos, ws.DataDir)
	server.SetNudge(loop.Nudge)

	return &App{
		cfg:    cfg,
		lock:   lock,
		store:  store,
		loop:   loop,
		server: server,
	}, nil
}

func (a *App) RunOnce(ctx context.Context) error { return a.loop.RunOnce(ctx) }

// Handler is the status page.
func (a *App) Handler() http.Handler { return a.server }

// Run ticks and serves until the context is cancelled. Two goroutines, not five (§3).
func (a *App) Run(ctx context.Context) error {
	srv := &http.Server{
		Addr:              net.JoinHostPort("127.0.0.1", strconv.Itoa(a.cfg.Port)),
		Handler:           a.server,
		ReadHeaderTimeout: 5 * time.Second,
		// An open log stream is an active connection until its request context is cancelled, and
		// Shutdown waits on active connections. Without this it waits out the whole grace period.
		BaseContext: func(net.Listener) context.Context { return ctx },
	}

	log.Printf("serving http://%s", srv.Addr)

	errs := make(chan error, 2)
	go func() { errs <- a.loop.Run(ctx) }()
	go func() {
		err := srv.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			errs <- nil
			return
		}
		errs <- fmt.Errorf("serve %s: %w", srv.Addr, err)
	}()

	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return errors.Join(srv.Shutdown(shutdown), <-errs, <-errs)
}

// Close releases the store and the flock.
func (a *App) Close() error { return errors.Join(a.store.Close(), a.lock.Close()) }
