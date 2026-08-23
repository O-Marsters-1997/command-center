// Package register holds the subcommands that exist only in an end-to-end build of cc. It is
// an ordinary untagged package so it compiles and lints like any other; cmd/cc/register_e2e.go
// is the build-tagged file that decides whether the release binary can reach it.
//
// Both commands exist to make a daemon scriptable: one drives the loop a fixed number of times
// without sleeping, the other reads the page out of a separate process.
package register

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
)

// Lookup returns the e2e-only subcommand named by args[0], or nil if args names none.
func Lookup(args []string) func(ctx context.Context, configPath string) error {
	if len(args) == 0 {
		return nil
	}
	rest := args[1:]
	switch args[0] {
	case "tick":
		return func(ctx context.Context, configPath string) error { return tick(ctx, configPath, rest) }
	case "request":
		return func(ctx context.Context, configPath string) error { return request(ctx, configPath, rest) }
	default:
		return nil
	}
}

// tick takes the flock and runs the loop exactly --count times with no sleeping, so a script
// asserts against a known number of ticks rather than against the wall clock.
func tick(ctx context.Context, configPath string, args []string) (err error) {
	flags := flag.NewFlagSet("tick", flag.ContinueOnError)
	count := flags.Int("count", 1, "number of ticks to run")
	if err := flags.Parse(args); err != nil {
		return err
	}

	app, err := cc.New(ctx, configPath)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, app.Close()) }()

	for range *count {
		if err := app.RunOnce(ctx); err != nil {
			return err
		}
	}
	return nil
}

// request prints the page a real HTTP client gets back from the real handler.
//
// It deliberately does not take the flock: a script needs to read the page while `cc tick` or a
// background daemon holds it. That is safe in Phase 1 only because no route mutates anything —
// it becomes an inv. 9 question as soon as one does.
func request(ctx context.Context, configPath string, args []string) (err error) {
	if len(args) != 2 {
		return fmt.Errorf("usage: cc request <method> <path>")
	}
	method, path := args[0], args[1]

	ws, err := cc.ResolveWorkspace(configPath)
	if err != nil {
		return err
	}
	store, err := cc.OpenStore(ws.DBPath)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, store.Close()) }()

	// httptest over an ephemeral port rather than the configured one: scripts run in parallel.
	server := httptest.NewServer(cc.NewServer(store, time.Now))
	defer server.Close()

	req, err := http.NewRequestWithContext(ctx, method, server.URL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Origin", server.URL)

	resp, err := server.Client().Do(req)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, resp.Body.Close()) }()

	if _, err := io.Copy(os.Stdout, resp.Body); err != nil {
		return err
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("%s %s: %s", method, path, resp.Status)
	}
	return nil
}
