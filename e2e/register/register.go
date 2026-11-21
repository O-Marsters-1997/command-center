package register

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/app"
	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web"
)

type subcommand = func(ctx context.Context, logger *slog.Logger, configPath string, env config.Env, args []string) error

func Commands() map[string]subcommand {
	return map[string]subcommand{
		"tick":    tick,
		"request": request,
		"import": func(ctx context.Context, _ *slog.Logger, configPath string, env config.Env, args []string) error {
			return importFeature(ctx, configPath, env, args)
		},
	}
}

func Seams() app.Seams {
	return app.Seams{Validate: SandboxValidate, RemoteOf: SandboxRemote}
}

func tick(ctx context.Context, logger *slog.Logger, configPath string, env config.Env, args []string) (err error) {
	flags := flag.NewFlagSet("tick", flag.ContinueOnError)
	count := flags.Int("count", 1, "number of ticks to run")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse tick flags: %w", err)
	}

	instance, err := app.New(logger, configPath, env, Seams())
	if err != nil {
		return fmt.Errorf("start app: %w", err)
	}
	defer func() { err = errors.Join(err, instance.Close()) }()

	for range *count {
		if err := instance.RunOnce(ctx); err != nil {
			return fmt.Errorf("run tick: %w", err)
		}
	}
	return nil
}

func importFeature(ctx context.Context, configPath string, env config.Env, args []string) error {
	if len(args) < 1 || len(args) > 2 {
		return errors.New("usage: cc import <feature> [owner/name]")
	}
	var repo string
	if len(args) == 2 {
		repo = args[1]
	}

	err := app.WithStore(configPath, env, func(_ config.Config, st *store.Store) error {
		if _, err := st.QueueVerbIntent(ctx, store.VerbRequest{
			Ticket: args[0], Verb: plan.VerbImport, Payload: repo, At: time.Now(),
		}); err != nil {
			return fmt.Errorf("queue import for %s: %w", args[0], err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("queue import: %w", err)
	}
	return nil
}

func request(
	ctx context.Context, logger *slog.Logger, configPath string, env config.Env, args []string,
) error {
	flags := flag.NewFlagSet("request", flag.ContinueOnError)
	origin := flags.String("origin", "", "Origin header to send (default: the server's own URL)")
	form := flags.String("form", "", "url-encoded body to post as application/x-www-form-urlencoded")
	htmx := flags.Bool("hx", false, "send HX-Request: true, as htmx does for a fragment")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse request flags: %w", err)
	}
	rest := flags.Args()
	if len(rest) != 2 {
		return errors.New("usage: cc request [-origin url] [-hx] [-form body] <method> <path>")
	}
	method, path := rest[0], rest[1]

	spec := requestSpec{method: method, path: path, form: *form, htmx: *htmx, origin: *origin}
	err := app.WithStore(configPath, env, func(cfg config.Config, st *store.Store) error {
		return send(ctx, logger, cfg, st, spec)
	})
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	return nil
}

type requestSpec struct {
	method, path, form, origin string
	htmx                       bool
}

func send(
	ctx context.Context, logger *slog.Logger, cfg config.Config, st *store.Store, spec requestSpec,
) (err error) {
	method, path, form, htmx, origin := spec.method, spec.path, spec.form, spec.htmx, spec.origin
	ws, err := config.ResolveWorkspace(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("resolve workspace: %w", err)
	}

	server := httptest.NewServer(web.NewServer(web.Deps{
		Logger: logger, Store: st, Clock: loop.RealClock{}, DataDir: ws.DataDir,
	}))
	defer server.Close()

	var body io.Reader
	if form != "" {
		body = strings.NewReader(form)
	}
	req, err := http.NewRequestWithContext(ctx, method, server.URL+path, body)
	if err != nil {
		return fmt.Errorf("build %s %s request: %w", method, path, err)
	}
	if form != "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	// No default: http.CrossOriginProtection allows a request carrying neither Origin nor
	// Sec-Fetch-Site, which is what a real non-browser client sends.
	if origin != "" {
		req.Header.Set("Origin", origin)
	}

	// http.Client follows a 303 by default.
	client := *server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send %s %s: %w", method, path, err)
	}
	defer func() { err = errors.Join(err, resp.Body.Close()) }()

	if _, err := io.Copy(os.Stdout, resp.Body); err != nil {
		return fmt.Errorf("print %s %s response: %w", method, path, err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("%s %s: %s", method, path, resp.Status)
	}
	return nil
}
