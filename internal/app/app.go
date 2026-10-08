package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/gh"
	"github.com/O-Marsters-1997/command-center/internal/git"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/runner"
	"github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web"
)

// App is one Command Centre instance: the flock, the store, the loop and the page.
type App struct {
	cfg    config.Config
	lock   *Flock
	store  *store.Store
	loop   *loop.Loop
	server *web.Server
}

type options struct {
	clock     loop.Clock
	observe   loop.ObserveFunc
	repoCheck RepoCheckFunc
	checkout  CheckoutFunc
}

type Option func(*options)

// WithClock replaces the real clock, making the rendered page byte-stable in tests.
func WithClock(clock loop.Clock) Option {
	return func(o *options) { o.clock = clock }
}

// WithObserver replaces the observe phase, so a tick runs without git or gh.
func WithObserver(observe loop.ObserveFunc) Option {
	return func(o *options) { o.observe = observe }
}

// RepoCheckFunc asserts the tracked repos' merge settings.
type RepoCheckFunc func(ctx context.Context, dataDir string, repos []store.Repo) error

// WithRepoCheck replaces the startup squash-only check, so a test runs without gh.
func WithRepoCheck(check RepoCheckFunc) Option {
	return func(o *options) { o.repoCheck = check }
}

// CheckoutFunc ensures every tracked repo has a working checkout under dataDir before the loop
// starts.
type CheckoutFunc func(ctx context.Context, dataDir string, repos []store.Repo) error

// WithCheckout replaces the startup checkout step.
func WithCheckout(checkout CheckoutFunc) Option {
	return func(o *options) { o.checkout = checkout }
}

func ensureAllCheckouts(ctx context.Context, dataDir string, repos []store.Repo) error {
	for _, repo := range repos {
		if err := git.EnsureCheckout(ctx, repo.Name, repo.Remote, config.CheckoutPath(dataDir, repo.Name)); err != nil {
			return err
		}
	}
	return nil
}

// New resolves the workspace, takes the flock, opens the store and imports any [[repo]] blocks
// into it on first boot. A second instance against the same workspace is refused.
func New(ctx context.Context, configPath string, opts ...Option) (app *App, err error) {
	settings := options{clock: loop.RealClock{}}
	for _, opt := range opts {
		opt(&settings)
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return nil, err
	}
	ws, err := config.ResolveWorkspace(cfg.DataDir)
	if err != nil {
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

	store, err := store.OpenStore(cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, store.Close())
		}
	}()

	if err := importLegacyRepos(ctx, store, configPath, cfg, settings.clock.Now()); err != nil {
		return nil, err
	}
	repos, err := store.Repos(ctx)
	if err != nil {
		return nil, err
	}
	checkout := settings.checkout
	if checkout == nil {
		checkout = ensureAllCheckouts
	}
	if err := checkout(ctx, cfg.DataDir, repos); err != nil {
		return nil, err
	}
	repoCheck := settings.repoCheck
	if repoCheck == nil {
		repoCheck = loop.AssertReposSquashOnly
	}
	if err := repoCheck(ctx, cfg.DataDir, repos); err != nil {
		return nil, err
	}

	if err := loop.WriteAgentFiles(ws); err != nil {
		return nil, err
	}

	observe := settings.observe
	if observe == nil {
		observe = loop.NewObserver(store, gh.CLI{}, cfg)
	}

	lp := loop.NewLoop(store, observe, settings.clock, cfg, ws, runner.ProcessRunner{})
	lp.SetForge(gh.CLI{})
	lp.SetWorktrees(git.CLI{})
	server := web.NewServer(store, settings.clock, ws.DataDir)
	server.SetNudge(lp.Nudge)
	server.SetSpendLimit5h(cfg.SpendLimit5h)
	server.SetBoardPollSeconds(cfg.BoardPollSeconds)

	return &App{
		cfg:    cfg,
		lock:   lock,
		store:  store,
		loop:   lp,
		server: server,
	}, nil
}

func (a *App) RunOnce(ctx context.Context) error { return a.loop.RunOnce(ctx) }

func (a *App) Handler() http.Handler { return a.server }

// Run ticks and serves until the context is cancelled.
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

func (a *App) Close() error { return errors.Join(a.store.Close(), a.lock.Close()) }
