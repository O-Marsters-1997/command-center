// Command cc runs the Command Centre: one reconcile loop plus one status page.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/O-Marsters-1997/command-center/internal/app"
)

func main() {
	config := flag.String("config", "cc/config.toml", "path to config file")
	flag.Parse()

	log.SetFlags(log.Ltime)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	command := subcmd(flag.Args())
	if command == nil && demoSubcmd != nil {
		command = demoSubcmd(flag.Args())
	}
	if command == nil {
		command = run
	}
	if err := command(ctx, *config); err != nil {
		log.Fatalf("cc: %v", err)
	}
}

var runOptions []app.Option

func run(ctx context.Context, configPath string) (err error) {
	log.Printf("config: %s", configPath)

	instance, err := app.New(ctx, configPath, runOptions...)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, instance.Close()) }()

	return instance.Run(ctx)
}

var demoSubcmd func(args []string) func(ctx context.Context, configPath string) error
