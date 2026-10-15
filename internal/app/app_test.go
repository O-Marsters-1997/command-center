package app_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/O-Marsters-1997/command-center/internal/app"
	"github.com/O-Marsters-1997/command-center/internal/cctest"
	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/gittest"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web/hx"
)

func TestNewRunsATickAndServesThePage(t *testing.T) {
	t.Parallel()
	configPath := appConfig(t)

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	observed := plan.Observation{
		PRs: map[plan.BranchRef]plan.PR{{Repo: sandboxRepo, Branch: "cc-1-first"}: {Number: 41, State: plan.Open}},
	}
	stub := func(context.Context) (plan.Observation, error) { return observed, nil }

	ctx := t.Context()
	inst, err := app.New(ctx, app.Deps{
		Logger: slog.New(slog.DiscardHandler), ConfigPath: configPath, Env: testEnv,
		Clock: cctest.Clock{At: at}, Observe: stub,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		if err := inst.Close(); err != nil {
			t.Errorf("close app: %v", err)
		}
	})

	if err := inst.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	cfg, err := config.Load(configPath, testEnv)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	serveCtx, stop := context.WithCancel(ctx)
	t.Cleanup(stop)
	done := make(chan error, 1)
	go func() { done <- inst.Run(serveCtx) }()
	terminated := false
	t.Cleanup(func() {
		stop()
		if terminated {
			return
		}
		if err := <-done; err != nil {
			t.Errorf("Run: %v", err)
		}
	})
	client := &http.Client{Timeout: 2 * time.Second}

	fetch := func(path string) string {
		t.Helper()
		url := fmt.Sprintf("http://127.0.0.1:%d%s", cfg.Port, path)
		deadline := time.After(5 * time.Second)
		for {
			req, err := http.NewRequestWithContext(serveCtx, http.MethodGet, url, nil)
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}
			req.Header.Set(hx.HeaderRequest, "true")
			resp, err := client.Do(req)
			if err == nil {
				body, readErr := io.ReadAll(resp.Body)
				if closeErr := resp.Body.Close(); closeErr != nil {
					t.Fatalf("Close(GET %s): %v", path, closeErr)
				}
				if readErr != nil {
					t.Fatalf("ReadAll(GET %s): %v", path, readErr)
				}
				if resp.StatusCode != http.StatusOK {
					t.Fatalf("GET %s: status = %d, want 200", path, resp.StatusCode)
				}
				return string(body)
			}
			select {
			case runErr := <-done:
				terminated = true
				t.Fatalf("Run before GET %s = %v", path, runErr)
			case <-deadline:
				t.Fatalf("GET %s did not connect: %v", path, err)
			case <-time.After(10 * time.Millisecond):
			}
		}
	}

	body := fetch("/tickets")
	for _, want := range []string{"sandbox://CC-1", "sandbox://CC-2", "ready"} {
		if !strings.Contains(body, want) {
			t.Errorf("page does not contain %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, ">blocked</span>") {
		t.Errorf("a row still renders blocked though its blocker has an open PR:\n%s", body)
	}
	if rail := fetch("/rail"); !strings.Contains(rail, "0s ago") {
		t.Errorf("rail does not carry the tick's age:\n%s", rail)
	}
}

func TestNewRefusesASecondInstance(t *testing.T) {
	t.Parallel()
	configPath := appConfig(t)

	ctx := t.Context()
	first, err := app.New(ctx, app.Deps{Logger: slog.New(slog.DiscardHandler), ConfigPath: configPath, Env: testEnv})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		if err := first.Close(); err != nil {
			t.Errorf("Close(first app): %v", err)
		}
	})

	if _, err := app.New(ctx, app.Deps{
		Logger: slog.New(slog.DiscardHandler), ConfigPath: configPath, Env: testEnv,
	}); err == nil {
		t.Fatal("a second instance started against the same workspace")
	}
}

const sandboxRepo = "sandbox-org/cc-sandbox"

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
			Name: sandboxRepo, Remote: filepath.Join(root, "remote.git"), Phase: plan.RepoReady, TrackedAt: time.Now(),
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

func TestRunReturnsNilOnACleanShutdown(t *testing.T) {
	t.Parallel()
	configPath := appConfig(t)

	stub := func(context.Context) (plan.Observation, error) { return plan.Observation{}, nil }
	ctx, cancel := context.WithCancel(t.Context())
	inst, err := app.New(ctx, app.Deps{
		Logger: slog.New(slog.DiscardHandler), ConfigPath: configPath, Env: testEnv, Observe: stub,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		if err := inst.Close(); err != nil {
			t.Errorf("Close(app): %v", err)
		}
	})

	done := make(chan error, 1)
	go func() { done <- inst.Run(ctx) }()
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run after cancellation = %v, want nil", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Run did not return within the shutdown budget")
	}
}

func TestRunReturnsTheBindErrorWithoutCancellation(t *testing.T) {
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

	stub := func(context.Context) (plan.Observation, error) { return plan.Observation{}, nil }
	inst, err := app.New(t.Context(), app.Deps{
		Logger: slog.New(slog.DiscardHandler), ConfigPath: configPath, Env: testEnv, Observe: stub,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		if err := inst.Close(); err != nil {
			t.Errorf("Close(app): %v", err)
		}
	})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- inst.Run(ctx) }()

	select {
	case err := <-done:
		if !errors.Is(err, syscall.EADDRINUSE) {
			t.Errorf("Run = %v, want the address-in-use error", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run kept running though the server could not bind")
	}
}

var testEnv = config.Env{Home: "/home/test", UserConfigDir: "/home/test/.config"}

func TestNewStartsWithNoTrackedRepos(t *testing.T) {
	t.Parallel()
	env := testEnv
	env.DataDir = t.TempDir()
	env.DatabaseURL = cctest.DSN(t)
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, nil, 0o600); err != nil {
		t.Fatalf("WriteFile(%s): %v", configPath, err)
	}

	inst, err := app.New(t.Context(), app.Deps{Logger: slog.New(slog.DiscardHandler), ConfigPath: configPath, Env: env})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		if err := inst.Close(); err != nil {
			t.Errorf("Close(app): %v", err)
		}
	})

	if err := inst.RunOnce(t.Context()); err != nil {
		t.Errorf("RunOnce with no repos = %v, want nil", err)
	}
}

func TestNewNamesTheStepThatFailed(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "absent.toml")

	_, err := app.New(t.Context(), app.Deps{Logger: slog.New(slog.DiscardHandler), ConfigPath: missing})

	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("New(missing config) = %v, want it to wrap os.ErrNotExist", err)
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
