package app_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/O-Marsters-1997/command-center/internal/adapter/gh/ghtest"
	"github.com/O-Marsters-1997/command-center/internal/adapter/git/gittest"
	"github.com/O-Marsters-1997/command-center/internal/adapter/runner/runnertest"
	"github.com/O-Marsters-1997/command-center/internal/app"
	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/cctest"
	"github.com/O-Marsters-1997/command-center/internal/web/hx"
	"github.com/O-Marsters-1997/command-center/internal/web/webtest"
)

const sandboxRepo = "sandbox-org/cc-sandbox"

var testEnv = config.Env{Home: "/home/test", UserConfigDir: "/home/test/.config"}

type manualClock struct{ at time.Time }

func (c manualClock) Now() time.Time                     { return c.at }
func (manualClock) After(time.Duration) <-chan time.Time { return nil }

func newApp(t *testing.T, configPath string, env config.Env, opts ...app.Option) *app.App {
	t.Helper()
	opts = append([]app.Option{app.WithForge(&ghtest.Forge{}), app.WithRunner(&runnertest.Runner{})}, opts...)
	inst, err := app.New(t.Context(), slog.New(slog.DiscardHandler), configPath, env, opts...)
	if err != nil {
		t.Fatalf("New(%s) error = %v", configPath, err)
	}
	t.Cleanup(func() {
		if err := inst.Close(); err != nil {
			t.Errorf("Close(app): %v", err)
		}
	})
	return inst
}

func appConfig(t *testing.T) string {
	t.Helper()
	dataDir := t.TempDir()

	repoPath, origin := gittest.WithOrigin(t)
	root := filepath.Dir(origin)
	checkout := config.CheckoutPath(dataDir, sandboxRepo)
	if err := os.MkdirAll(filepath.Dir(checkout), 0o700); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(checkout), err)
	}
	if err := os.Rename(repoPath, checkout); err != nil {
		t.Fatalf("Rename(%s, %s): %v", repoPath, checkout, err)
	}
	configPath := filepath.Join(root, "command-centre.toml")
	dsn := cctest.DSN(t)
	body := fmt.Sprintf("port = %d\ndata_dir = %q\ndatabase_url = %q\n", freePort(t), dataDir, dsn)
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile(%s): %v", configPath, err)
	}
	seed(t, dsn, func(db *store.Store) error {
		repo := store.Repo{
			Name: sandboxRepo, Remote: origin, Phase: plan.RepoReady,
			TrackedAt: time.Date(2026, 8, 20, 11, 0, 0, 0, time.UTC),
		}
		if err := db.UpsertRepo(t.Context(), repo); err != nil {
			return err
		}
		return db.UpsertTickets(t.Context(), []store.Ticket{
			{URL: "sandbox://CC-1", Repo: sandboxRepo, Branch: "cc-1-first"},
			{URL: "sandbox://CC-2", Repo: sandboxRepo, Branch: "cc-2-second", BlockedBy: []string{"sandbox://CC-1"}},
		})
	})
	return configPath
}

func seed(t *testing.T, dsn string, write func(*store.Store) error) {
	t.Helper()
	db, err := store.OpenStore(dsn, store.MaxOpenConns(4))
	if err != nil {
		t.Fatalf("OpenStore(%s): %v", dsn, err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("Close(seed store): %v", err)
		}
	}()
	if err := write(db); err != nil {
		t.Fatalf("write(seed store): %v", err)
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find a free port: %v", err)
	}
	defer func() {
		if err := l.Close(); err != nil {
			t.Errorf("Close(free-port listener): %v", err)
		}
	}()
	return l.Addr().(*net.TCPAddr).Port
}

func get(t *testing.T, inst *app.App, path string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set(hx.HeaderRequest, "true")
	rec := httptest.NewRecorder()
	inst.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: status = %d, want 200", path, rec.Code)
	}
	return rec.Body.String()
}

func TestNew(t *testing.T) {
	t.Parallel()

	t.Run("runs a tick and serves the page", func(t *testing.T) {
		t.Parallel()
		at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
		observed := plan.Observation{
			PRs: map[plan.BranchRef]plan.PR{{Repo: sandboxRepo, Branch: "cc-1-first"}: {Number: 41, State: plan.Open}},
		}
		stub := func(context.Context) (plan.Observation, error) { return observed, nil }
		inst := newApp(t, appConfig(t), testEnv, app.WithClock(manualClock{at}), app.WithObserver(stub))

		if err := inst.RunOnce(t.Context()); err != nil {
			t.Fatalf("RunOnce() error = %v", err)
		}

		page := get(t, inst, "/tickets")
		if got := webtest.RowFor(t, page, "sandbox://CC-2").State; got != "Ready to launch" {
			t.Errorf("GET /tickets: CC-2 state = %q, want %q once its blocker has an open PR", got, "Ready to launch")
		}
		if rail := get(t, inst, "/rail"); !strings.Contains(rail, "0s ago") {
			t.Errorf("GET /rail does not carry the tick's age:\n%s", rail)
		}
	})

	t.Run("refuses a second instance", func(t *testing.T) {
		t.Parallel()
		configPath := appConfig(t)
		newApp(t, configPath, testEnv)

		logger := slog.New(slog.DiscardHandler)
		if _, err := app.New(t.Context(), logger, configPath, testEnv); !errors.Is(err, app.ErrLocked) {
			t.Errorf("New(second instance) error = %v, want %v", err, app.ErrLocked)
		}
	})

	t.Run("starts with no tracked repos", func(t *testing.T) {
		t.Parallel()
		env := testEnv
		env.DataDir = t.TempDir()
		env.DatabaseURL = cctest.DSN(t)
		configPath := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(configPath, nil, 0o600); err != nil {
			t.Fatalf("WriteFile(%s): %v", configPath, err)
		}
		inst := newApp(t, configPath, env)

		if err := inst.RunOnce(t.Context()); err != nil {
			t.Errorf("RunOnce() with no repos = %v, want nil", err)
		}
	})

	t.Run("a missing config wraps ErrNotExist", func(t *testing.T) {
		t.Parallel()
		missing := filepath.Join(t.TempDir(), "absent.toml")

		_, err := app.New(t.Context(), slog.New(slog.DiscardHandler), missing, config.Env{})
		if !errors.Is(err, os.ErrNotExist) {
			t.Errorf("New(%s) error = %v, want %v", missing, err, os.ErrNotExist)
		}
	})
}

func TestApp_Run(t *testing.T) {
	t.Parallel()
	idle := func(context.Context) (plan.Observation, error) { return plan.Observation{}, nil }

	t.Run("returns nil on a clean shutdown", func(t *testing.T) {
		t.Parallel()
		inst := newApp(t, appConfig(t), testEnv, app.WithObserver(idle))
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { done <- inst.Run(ctx) }()

		cancel()
		if err := <-done; err != nil {
			t.Errorf("Run() after cancellation = %v, want nil", err)
		}
	})

	t.Run("returns the bind error without cancellation", func(t *testing.T) {
		t.Parallel()
		configPath := appConfig(t)
		held, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("hold a port: %v", err)
		}
		t.Cleanup(func() {
			if err := held.Close(); err != nil {
				t.Errorf("Close(listener): %v", err)
			}
		})
		port := held.Addr().(*net.TCPAddr).Port
		body, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatalf("ReadFile(%s): %v", configPath, err)
		}
		body = regexp.MustCompile(`port = \d+\n`).ReplaceAll(body, []byte("port = "+strconv.Itoa(port)+"\n"))
		if err := os.WriteFile(configPath, body, 0o600); err != nil {
			t.Fatalf("WriteFile(%s): %v", configPath, err)
		}
		inst := newApp(t, configPath, testEnv, app.WithObserver(idle))

		if err := inst.Run(t.Context()); !errors.Is(err, syscall.EADDRINUSE) {
			t.Errorf("Run() = %v, want %v", err, syscall.EADDRINUSE)
		}
	})
}
