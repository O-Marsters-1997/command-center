//go:build demo

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/demo"
)

func demoSubcmd(args []string) command {
	if len(args) == 0 || args[0] != "demo" {
		return nil
	}
	rest := args[1:]
	return func(ctx context.Context, logger *slog.Logger, _ string, _ config.Env) error {
		return runDemo(ctx, logger, rest)
	}
}

func runDemo(ctx context.Context, logger *slog.Logger, args []string) error {
	flags := flag.NewFlagSet("demo", flag.ContinueOnError)
	seed := flags.Int64("seed", time.Now().UnixNano(), "seed for the generated board; defaults to the time")
	tickets := flags.Int("tickets", 12, "number of tickets to generate")
	speed := flags.Float64("speed", 1, "sim seconds per real second")
	port := flags.Int("port", 7777, "port to serve the board on")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse demo flags: %w", err)
	}
	if flags.NArg() > 0 || *tickets < 1 || !(*speed > 0 && *speed <= 1000) {
		return errors.New("usage: cc demo [--seed N] [--tickets N] [--speed X] [--port N]")
	}

	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(*port))
	logger.Info("serving demo", "seed", *seed, "addr", "http://"+addr)
	if err := demo.Serve(ctx, logger, demo.Generate(*seed, *tickets), *speed, addr); err != nil {
		return fmt.Errorf("serve demo: %w", err)
	}
	return nil
}
