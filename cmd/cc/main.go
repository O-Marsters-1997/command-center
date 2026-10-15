package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/O-Marsters-1997/command-center/internal/app"
	"github.com/O-Marsters-1997/command-center/internal/config"
)

func main() {
	configPath := flag.String("config", "cc/config.toml", "path to config file")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	env, err := config.EnvFrom(os.Getenv)
	if err != nil {
		logger.Error("cc exited", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	deps := app.Deps{Logger: logger, ConfigPath: *configPath, Env: env}

	var runErr error
	switch flag.Arg(0) {
	case "open":
		runErr = openCommand(ctx, deps)
	case "demo":
		runErr = demoCommand(ctx, deps)
	case "tick", "request", "import":
		runErr = e2eCommand(ctx, deps, flag.Args())
	default:
		runErr = run(ctx, deps)
	}
	if err := runErr; err != nil {
		logger.Error("cc exited", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, deps app.Deps) (err error) {
	deps.Logger.Info("loading config", "path", deps.ConfigPath)

	instance, err := newApp(ctx, deps)
	if err != nil {
		return fmt.Errorf("start app: %w", err)
	}
	defer func() { err = errors.Join(err, instance.Close()) }()

	if err := instance.Run(ctx); err != nil {
		return fmt.Errorf("run app: %w", err)
	}
	return nil
}
