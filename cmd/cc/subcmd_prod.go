//go:build !e2e

package main

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
)

// subcmd resolves the release binary's one subcommand, open. `cc tick` and `cc request` exist
// only under -tags=e2e.
func subcmd(args []string) func(ctx context.Context, configPath string) error {
	if len(args) == 0 || args[0] != "open" {
		return nil
	}
	return open
}

func open(ctx context.Context, configPath string) error {
	cfg, err := cc.LoadConfig(configPath)
	if err != nil {
		return err
	}
	dir, err := os.Getwd()
	if err != nil {
		return err
	}

	target := fmt.Sprintf("http://127.0.0.1:%d/", cfg.Port)
	if name, ok := cc.RepoNameForDir(ctx, dir, cfg.Repos); ok {
		target += "?repo=" + url.QueryEscape(name)
	} else {
		fmt.Fprintln(os.Stderr, "cc open: no configured repo matches this directory; falling back to the unscoped board")
	}

	if !daemonListening(cfg.Port) {
		fmt.Fprintf(os.Stderr, "cc open: no daemon listening on port %d\n", cfg.Port)
		fmt.Println(target)
		return nil
	}

	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	return exec.CommandContext(ctx, opener, target).Run()
}

func daemonListening(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
