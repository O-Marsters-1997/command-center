package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"golang.org/x/sync/errgroup"

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
	logger *slog.Logger
	lock   *Flock
	store  *store.Store
	loop   *loop.Loop
	server *web.Server
}

type options struct {
	clock    loop.Clock
	observe  loop.ObserveFunc
	validate loop.ValidateFunc
	remoteOf loop.RemoteFunc
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

// WithValidator replaces how a cloning repo is settled into ready or refused, so a test or the
// e2e build runs without GitHub.
func WithValidator(validate loop.ValidateFunc) Option {
	return func(o *options) { o.validate = validate }
}

// WithRemoteSource replaces the gh lookup of a tracked repo's clone URL, so a test or the e2e
// build runs without GitHub.
func WithRemoteSource(remoteOf loop.RemoteFunc) Option {
	return func(o *options) { o.remoteOf = remoteOf }
}

func New(ctx context.Context, logger *slog.Logger, configPath string, opts ...Option) (app *App, err error) {
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

	store, err := store.OpenStore(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, store.Close())
		}
	}()

	if err := loop.WriteAgentFiles(ws); err != nil {
		return nil, err
	}

	observe := settings.observe
	if observe == nil {
		observe = loop.NewObserver(store, gh.CLI{}, cfg)
	}

	lp := loop.NewLoop(logger, store, observe, settings.clock, cfg, ws, runner.ProcessRunner{})
	lp.SetForge(gh.CLI{})
	lp.SetWorktrees(git.CLI{})
	if settings.validate != nil {
		lp.SetValidator(settings.validate)
	}
	if settings.remoteOf != nil {
		lp.SetRemoteSource(settings.remoteOf)
	}
	server := web.NewServer(logger, store, settings.clock, ws.DataDir)
	server.SetNudge(lp.Nudge)
	server.SetSpendLimit5h(cfg.SpendLimit5h)
	server.SetBoardPollSeconds(cfg.BoardPollSeconds)

	return &App{
		cfg:    cfg,
		logger: logger,
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
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	g, ctx := errgroup.WithContext(ctx)

	srv := &http.Server{
		Addr:              net.JoinHostPort("127.0.0.1", strconv.Itoa(a.cfg.Port)),
		Handler:           a.server,
		ReadHeaderTimeout: 5 * time.Second,
		// An open log stream is an active connection until its request context is cancelled, and
		// Shutdown waits on active connections. Without this it waits out the whole grace period.
		BaseContext: func(net.Listener) context.Context { return ctx },
	}

	a.logger.Info("serving", "addr", "http://"+srv.Addr)

	g.Go(func() error {
		defer stop()
		return a.loop.Run(ctx)
	})
	g.Go(func() error {
		defer stop()
		err := srv.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve %s: %w", srv.Addr, err)
	})
	g.Go(func() error {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdown)
	})

	return g.Wait()
}

func (a *App) Close() error { return errors.Join(a.store.Close(), a.lock.Close()) }
