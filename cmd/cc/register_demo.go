//go:build demo

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"strconv"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/demo"
)

func init() { demoSubcmd = lookupDemo }

func lookupDemo(args []string) func(ctx context.Context, configPath string) error {
	if len(args) == 0 || args[0] != "demo" {
		return nil
	}
	rest := args[1:]
	return func(ctx context.Context, _ string) error { return runDemo(ctx, rest) }
}

func runDemo(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("demo", flag.ContinueOnError)
	seed := flags.Int64("seed", time.Now().UnixNano(), "seed for the generated board; defaults to the time")
	tickets := flags.Int("tickets", 12, "number of tickets to generate")
	speed := flags.Float64("speed", 1, "sim seconds per real second")
	port := flags.Int("port", 7777, "port to serve the board on")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 || *tickets < 1 || !(*speed > 0 && *speed <= 1000) {
		return fmt.Errorf("usage: cc demo [--seed N] [--tickets N] [--speed X] [--port N]")
	}

	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(*port))
	log.Printf("demo: seed %d, serving http://%s", *seed, addr)
	return demo.Serve(ctx, demo.Generate(*seed, *tickets), *speed, addr)
}
