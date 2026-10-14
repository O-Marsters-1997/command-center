// Command cc runs the Command Centre: one reconcile loop plus one status page.
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

type command func(ctx context.Context, logger *slog.Logger, configPath string, env config.Env) error

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

	cmd := subcmd(flag.Args())
	if cmd == nil {
		cmd = demoSubcmd(flag.Args())
	}
	if cmd == nil {
		cmd = run
	}
	if err := cmd(ctx, logger, *configPath, env); err != nil {
		logger.Error("cc exited", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger, configPath string, env config.Env) (err error) {
	logger.Info("loading config", "path", configPath)

	instance, err := app.New(ctx, logger, configPath, env, appOptions()...)
	if err != nil {
		return fmt.Errorf("start app: %w", err)
	}
	defer func() { err = errors.Join(err, instance.Close()) }()

	if err := instance.Run(ctx); err != nil {
		return fmt.Errorf("run app: %w", err)
	}
	return nil
}
