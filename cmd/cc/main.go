// Command cc runs the Command Centre: one reconcile loop plus one status page.
// See docs/command-centre-v1.md.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/O-Marsters-1997/command-center/internal/cc"
)

func main() {
	config := flag.String("config", ".claude/command-centre.toml", "path to config file")
	flag.Parse()

	log.SetFlags(log.Ltime)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	command := subcmd(flag.Args())
	if command == nil {
		command = run
	}
	if err := command(ctx, *config); err != nil {
		log.Fatalf("cc: %v", err)
	}
}

func run(ctx context.Context, configPath string) (err error) {
	log.Printf("config: %s", configPath)

	app, err := cc.New(ctx, configPath)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, app.Close()) }()

	return app.Run(ctx)
}
