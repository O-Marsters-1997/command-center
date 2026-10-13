//go:build !e2e

package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

func subcmd(args []string) func(ctx context.Context, configPath string) error {
	if len(args) == 0 {
		return nil
	}
	switch args[0] {
	case "open":
		return open
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
		owner, repo, _ := strings.Cut(name, "/")
		target += "repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo)
	} else {
		fmt.Fprintln(os.Stderr, "cc open: no tracked repo matches this directory; falling back to the home page")
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

func daemonListening(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
