//go:build demo

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/O-Marsters-1997/command-center/internal/demo"
)

const scenarioDir = "demo/scenarios"

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
	speed := flags.Float64("speed", 0, "sim seconds per real second; defaults to the scenario's, then 20")
	until := flags.Duration("until", 0, "pause once the sim reaches this time")
	port := flags.Int("port", 7777, "port to serve the board on")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: cc demo [--speed N] [--until T] [--port N] <scenario>")
	}

	name := flags.Arg(0)
	path := name
	if !strings.HasSuffix(name, ".toml") {
		path = filepath.Join(scenarioDir, name+".toml")
	}
	scenario, err := demo.LoadScenario(path)
	if err != nil {
		return err
	}
	player, err := demo.NewPlayer(ctx, scenario, *speed, *until)
	if err != nil {
		return err
	}
	defer func() {
		if err := player.Close(); err != nil {
			log.Printf("demo: close: %v", err)
		}
	}()

	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(*port))
	log.Printf("demo %s: serving http://%s", name, addr)
	return player.Serve(ctx, addr)
}
