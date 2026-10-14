//go:build !e2e

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/app"
	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

func subcmd(args []string) command {
	if len(args) == 0 {
		return nil
	}
	switch args[0] {
	case "open":
		return func(ctx context.Context, _ *slog.Logger, configPath string, env config.Env) error {
			return open(ctx, configPath, env, os.Stdout, os.Stderr)
		}
	default:
		return nil
	}
}

func appOptions() []app.Option { return nil }

func open(ctx context.Context, configPath string, env config.Env, stdout, stderr io.Writer) error {
	cfg, err := config.LoadConfig(configPath, env)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	dir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	repos, err := trackedRepos(ctx, cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintf(stderr, "cc open: cannot read tracked repos: %v\n", err)
	}

	target := fmt.Sprintf("http://127.0.0.1:%d/", cfg.Port)
	if name, ok := loop.RepoNameForDir(ctx, dir, repos); ok {
		owner, repo, _ := strings.Cut(name, "/")
		target += "repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo)
	} else {
		fmt.Fprintln(stderr, "cc open: no tracked repo matches this directory; falling back to the home page")
	}

	if !daemonListening(cfg.Port) {
		fmt.Fprintf(stderr, "cc open: no daemon listening on port %d\n", cfg.Port)
		fmt.Fprintln(stdout, target)
		return nil
	}

	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	if err := exec.CommandContext(ctx, opener, target).Run(); err != nil {
		return fmt.Errorf("run %s %s: %w", opener, target, err)
	}
	return nil
}

func trackedRepos(ctx context.Context, databaseURL string) (_ []store.Repo, err error) {
	st, err := store.OpenStore(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}
	defer func() { err = errors.Join(err, st.Close()) }()
	repos, err := st.Repos(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tracked repos: %w", err)
	}
	return repos, nil
}

func daemonListening(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
