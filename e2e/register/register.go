// Package register holds the subcommands that exist only in an end-to-end build of cc. It is
// an ordinary untagged package so it compiles and lints like any other; cmd/cc/register_e2e.go
// is the build-tagged file that decides whether the release binary can reach it.
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
	"strings"
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

// request prints the page a real HTTP client gets back from the real handler. It deliberately
// does not take the flock: a script reads while `cc tick` or a daemon holds it. POST /launch's
// write is a safe blind SQLite INSERT under WAL; a future verb doing more would revisit inv. 9.
func request(ctx context.Context, configPath string, args []string) (err error) {
	flags := flag.NewFlagSet("request", flag.ContinueOnError)
	origin := flags.String("origin", "", "Origin header to send (default: the server's own URL)")
	// -form is how a script drives a route the way the page's own forms do, with the fields in
	// the body: a path-only `cc request POST /verb` cannot express that.
	form := flags.String("form", "", "url-encoded body to post as application/x-www-form-urlencoded")
	if err := flags.Parse(args); err != nil {
		return err
	}
	rest := flags.Args()
	if len(rest) != 2 {
		return fmt.Errorf("usage: cc request [-origin url] <method> <path>")
	}
	method, path := rest[0], rest[1]

	cfg, err := cc.LoadConfig(configPath)
	if err != nil {
		return err
	}
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
	server := httptest.NewServer(cc.NewServer(store, time.Now, cfg.Repos, ws.Root))
	defer server.Close()

	var body io.Reader
	if *form != "" {
		body = strings.NewReader(*form)
	}
	req, err := http.NewRequestWithContext(ctx, method, server.URL+path, body)
	if err != nil {
		return err
	}
	if *form != "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	requestOrigin := *origin
	if requestOrigin == "" {
		requestOrigin = server.URL
	}
	req.Header.Set("Origin", requestOrigin)

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
