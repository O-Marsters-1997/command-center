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

	"github.com/O-Marsters-1997/command-center/internal/adapter/gh"
	"github.com/O-Marsters-1997/command-center/internal/adapter/notify"
	"github.com/O-Marsters-1997/command-center/internal/adapter/runner"
	"github.com/O-Marsters-1997/command-center/internal/adapter/tracker"
	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/loop"
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

type Seams struct {
	Clock    loop.Clock
	Observe  loop.ObserveFunc
	Validate loop.ValidateFunc
	RemoteOf loop.RemoteFunc
	Forge    gh.Forge
	Runner   runner.Runner
}

func New(logger *slog.Logger, configPath string, env config.Env, seams Seams) (app *App, err error) {
	if seams.Clock == nil {
		seams.Clock = loop.RealClock{}
	}
	if seams.Forge == nil {
		seams.Forge = gh.CLI{}
	}
	if seams.Runner == nil {
		seams.Runner = runner.ProcessRunner{}
	}
	if seams.RemoteOf == nil {
		seams.RemoteOf = gh.CLI{}.RepoRemote
	}

	cfg, err := config.Load(configPath, env)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	ws, err := config.ResolveWorkspace(cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace: %w", err)
	}
	lock, err := Lock(ws.LockPath)
	if err != nil {
		return nil, fmt.Errorf("lock workspace: %w", err)
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, lock.Close())
		}
	}()

	st, err := store.OpenStore(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, st.Close())
		}
	}()

	if err := loop.WriteAgentFiles(ws); err != nil {
		return nil, fmt.Errorf("write agent files: %w", err)
	}

	if seams.Observe == nil {
		seams.Observe = loop.NewObserver(st, seams.Forge, cfg)
	}

	loopOpts := []loop.Option{
		loop.WithForge(seams.Forge), loop.WithNotifier(notify.Log{Logger: logger}),
		loop.WithRemoteOf(seams.RemoteOf), loop.WithTracker(tracker.Resolve(gh.CLI{})),
	}
	if seams.Validate != nil {
		loopOpts = append(loopOpts, loop.WithValidator(seams.Validate))
	}
	lp := loop.New(loop.Deps{
		Logger: logger, Store: st, Clock: seams.Clock, Runner: seams.Runner, Observe: seams.Observe,
		Config: cfg, Workspace: ws,
	}, loopOpts...)
	server := web.NewServer(web.Deps{
		Logger: logger, Store: st, Clock: seams.Clock, DataDir: ws.DataDir, Nudger: lp.Nudger(),
		SpendLimit5h: cfg.SpendLimit5h, BoardPollSeconds: cfg.BoardPollSeconds,
		AgentCommand: cfg.AgentCommand,
	})

	return &App{
		cfg:    cfg,
		logger: logger,
		lock:   lock,
		store:  st,
		loop:   lp,
		server: server,
	}, nil
}

func (a *App) RunOnce(ctx context.Context) error {
	if err := a.loop.RunOnce(ctx); err != nil {
		return fmt.Errorf("run loop once: %w", err)
	}
	return nil
}

func (a *App) Handler() http.Handler { return a.server }

func (a *App) Run(ctx context.Context) error {
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	g, ctx := errgroup.WithContext(ctx)

	srv := &http.Server{
		Addr:              net.JoinHostPort("127.0.0.1", strconv.Itoa(a.cfg.Port)),
		Handler:           a.server,
		ReadHeaderTimeout: 5 * time.Second,
		// net/http Shutdown waits for active streams; cancel their request contexts first.
		BaseContext: func(net.Listener) context.Context { return ctx },
	}

	a.logger.Info("serving", "addr", "http://"+srv.Addr)

	g.Go(func() error {
		defer stop()
		if err := a.loop.Run(ctx); err != nil {
			return fmt.Errorf("run loop: %w", err)
		}
		return nil
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
		if err := srv.Shutdown(shutdown); err != nil {
			return fmt.Errorf("shut down server: %w", err)
		}
		return nil
	})

	if err := g.Wait(); err != nil {
		return fmt.Errorf("run services: %w", err)
	}
	return nil
}

func (a *App) Close() error { return errors.Join(a.store.Close(), a.lock.Close()) }
