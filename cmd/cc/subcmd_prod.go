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
	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

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
	case "passwd":
		return func(ctx context.Context, configPath string) error { return passwd(ctx, configPath, rest) }
	default:
		return nil
	}
}

func open(ctx context.Context, configPath string) error {
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return err
	}
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	repos, err := trackedRepos(ctx, cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cc open: cannot read tracked repos: %v\n", err)
	}

	target := fmt.Sprintf("http://127.0.0.1:%d/", cfg.Port)
	if name, ok := loop.RepoNameForDir(ctx, dir, repos); ok {
		target += "?repo=" + url.QueryEscape(name)
	} else {
		fmt.Fprintln(os.Stderr, "cc open: no tracked repo matches this directory; falling back to the unscoped board")
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

func trackedRepos(ctx context.Context, databaseURL string) (_ []store.Repo, err error) {
	st, err := store.OpenStore(databaseURL)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, st.Close()) }()
	return st.Repos(ctx)
}

func useradd(ctx context.Context, configPath string, args []string) error {
	return setPassword(ctx, configPath, "useradd", args,
		func(ctx context.Context, st *store.Store, email, hash string) error {
			return st.CreateUser(ctx, email, hash, time.Now())
		})
}

func passwd(ctx context.Context, configPath string, args []string) error {
	return setPassword(ctx, configPath, "passwd", args,
		func(ctx context.Context, st *store.Store, email, hash string) error {
			return st.SetPassword(ctx, email, hash)
		})
}

func setPassword(
	ctx context.Context, configPath, name string, args []string,
	write func(ctx context.Context, st *store.Store, email, hash string) error,
) (err error) {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: cc %s <email>", name)
	}
	email := flags.Arg(0)

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return err
	}
	st, err := store.OpenStore(cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, st.Close()) }()

	password, err := auth.GeneratePassword()
	if err != nil {
		return err
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	if err := write(ctx, st, email, hash); err != nil {
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
