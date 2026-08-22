// Command cc runs the Command Centre: one reconcile loop plus one status page.
// See docs/command-centre-v1.md.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	config := flag.String("config", ".claude/command-centre.toml", "path to config file")
	flag.Parse()

	log.SetFlags(log.Ltime)
	log.Printf("config: %s", *config)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	<-ctx.Done()
}
