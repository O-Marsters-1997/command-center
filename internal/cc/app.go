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
	now       func() time.Time
	observe   ObserveFunc
	repoCheck RepoCheckFunc
	runner    Runner
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

// New resolves the workspace, takes the flock, opens the store and upserts the configured
// tasks. A second instance against the same workspace is refused (inv. 9).
func New(ctx context.Context, configPath string, opts ...Option) (app *App, err error) {
	settings := options{now: time.Now}
	for _, opt := range opts {
		opt(&settings)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		return nil, err
	}
	ws, err := ResolveWorkspace(configPath)
	if err != nil {
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

	store, err := OpenStore(ws.DBPath)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, store.Close())
		}
	}()

	// Intake is upserted at startup only, so the tick never adds rows to its own table.
	if err := store.UpsertTasks(ctx, cfg.Tasks); err != nil {
		return nil, err
	}

	// Written once at startup rather than per spawn: the content never varies, and every spawn
	// just passes the same path (inv. 17).
	if err := WriteAgentSettings(ws.SettingsPath); err != nil {
		return nil, err
	}

	observe := settings.observe
	if observe == nil {
		observe = NewObserver(store, cfg, ws.Root)
	}
	runner := settings.runner
	if runner == nil {
		runner = ProcessRunner{}
	}
	return &App{
		cfg:    cfg,
		lock:   lock,
		store:  store,
		loop:   NewLoop(store, observe, settings.now, cfg, ws, runner),
		server: NewServer(store, settings.now, cfg.Repos),
	}, nil
}

// RunOnce runs a single tick.
func (a *App) RunOnce(ctx context.Context) error { return a.loop.RunOnce(ctx) }

// Handler is the status page.
func (a *App) Handler() http.Handler { return a.server }

// Run ticks and serves until the context is cancelled. Two goroutines, not five (§3).
func (a *App) Run(ctx context.Context) error {
	srv := &http.Server{
		Addr:              net.JoinHostPort("127.0.0.1", strconv.Itoa(a.cfg.Port)),
		Handler:           a.server,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("serving http://%s", srv.Addr)

	errs := make(chan error, 2)
	go func() { errs <- a.loop.Run(ctx) }()
	go func() {
		err := srv.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
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
