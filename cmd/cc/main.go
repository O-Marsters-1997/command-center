// Command cc runs the Command Centre: one reconcile loop plus one status page.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/O-Marsters-1997/command-center/internal/app"
)

type command func(ctx context.Context, logger *slog.Logger, configPath string) error

func main() {
	config := flag.String("config", "cc/config.toml", "path to config file")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cmd := subcmd(flag.Args())
	if cmd == nil && demoSubcmd != nil {
		cmd = demoSubcmd(flag.Args())
	}
	if cmd == nil {
		cmd = run
	}
	if err := cmd(ctx, logger, *config); err != nil {
		logger.Error("cc exited", "err", err)
		os.Exit(1)
	}
}

var runOptions []app.Option

func run(ctx context.Context, logger *slog.Logger, configPath string) (err error) {
	logger.Info("loading config", "path", configPath)

	instance, err := app.New(ctx, logger, configPath, runOptions...)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, instance.Close()) }()

	return instance.Run(ctx)
}

var demoSubcmd func(args []string) command
