//go:build !e2e

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/auth"
	"github.com/O-Marsters-1997/command-center/internal/cc"
)

// subcmd resolves the release binary's subcommands, open and useradd. `cc tick` and `cc request`
// exist only under -tags=e2e.
func subcmd(args []string) func(ctx context.Context, configPath string) error {
	if len(args) == 0 {
		return nil
	}
	rest := args[1:]
	switch args[0] {
	case "open":
		return open
	case "useradd":
		return func(ctx context.Context, configPath string) error { return useradd(ctx, configPath, rest) }
	default:
		return nil
	}
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

func useradd(ctx context.Context, configPath string, args []string) (err error) {
	flags := flag.NewFlagSet("useradd", flag.ContinueOnError)
	if err := flags.Parse(args); err != nil {
		return err
	}
	rest := flags.Args()
	if len(rest) != 1 {
		return fmt.Errorf("usage: cc useradd <email>")
	}
	email := rest[0]

	cfg, err := cc.LoadConfig(configPath)
	if err != nil {
		return err
	}
	store, err := cc.OpenStore(cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, store.Close()) }()

	password, err := auth.GeneratePassword()
	if err != nil {
		return err
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	if err := store.CreateUser(ctx, email, hash, time.Now()); err != nil {
		return err
	}

	fmt.Println(password)
	return nil
}

func daemonListening(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
