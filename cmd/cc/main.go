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

type command func(ctx context.Context, logger *slog.Logger, configPath string, env config.Env, args []string) error

var (
	subcommands = map[string]command{
		"open":         openSubcmd,
		"record-usage": recordUsageSubcmd,
	}
	seams app.Seams
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

	cmd, ok := subcommands[flag.Arg(0)]
	if !ok {
		cmd = run
	}
	if err := cmd(ctx, logger, *configPath, env, flag.Args()[min(1, flag.NArg()):]); err != nil {
		logger.Error("cc exited", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger, configPath string, env config.Env, _ []string) (err error) {
	logger.Info("loading config", "path", configPath)

	instance, err := app.New(logger, configPath, env, seams)
	if err != nil {
		return fmt.Errorf("start app: %w", err)
	}
	defer func() { err = errors.Join(err, instance.Close()) }()

	if err := instance.Run(ctx); err != nil {
		return fmt.Errorf("run app: %w", err)
	}
	return nil
}
