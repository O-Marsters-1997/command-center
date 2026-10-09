package app_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/O-Marsters-1997/command-center/internal/app"
	"github.com/O-Marsters-1997/command-center/internal/cctest"
	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

func TestNewRunsATickAndServesThePage(t *testing.T) {
	configPath := appConfig(t)

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	observed := plan.Observation{
		PRs: map[string]plan.PR{sandboxRepo + "//cc-1-first": {Number: 41, State: plan.Open}},
	}
	stub := func(context.Context) (plan.Observation, error) { return observed, nil }

	ctx := t.Context()
	inst, err := app.New(ctx, configPath, app.WithClock(fixedClock(at)), app.WithObserver(stub))
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

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.OpenStore(cfg.DatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	token, err := st.SeedSession(ctx, "me@example.com", at, at.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "cc_session", Value: token})
	rec := httptest.NewRecorder()
	inst.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	body := rec.Body.String()
	// The two tickets seeded straight into the store before New both derive from the stub's
	// snapshot: CC-1 has no blockers, CC-2's blocker now has an open PR.
	for _, want := range []string{"sandbox://CC-1", "sandbox://CC-2", "ready", "0s ago"} {
		if !strings.Contains(body, want) {
			t.Errorf("page does not contain %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "blocked") {
		t.Errorf("a row still renders blocked though its blocker has an open PR:\n%s", body)
	}
}

func TestNewRefusesASecondInstance(t *testing.T) {
	configPath := appConfig(t)

	ctx := t.Context()
	first, err := app.New(ctx, configPath)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = first.Close() })

	if _, err := app.New(ctx, configPath); err == nil {
		t.Fatal("a second instance started against the same workspace")
	}
}

const sandboxRepo = "sandbox-org/cc-sandbox"

func appConfig(t *testing.T) string {
	t.Helper()
	dataDir := t.TempDir()
	t.Setenv("CC_DATA_DIR", dataDir)

	root, repoPath := repoWithOrigin(t)
	checkout := config.CheckoutPath(dataDir, sandboxRepo)
	if err := os.MkdirAll(filepath.Dir(checkout), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(repoPath, checkout); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "command-centre.toml")
	if err := os.WriteFile(configPath, []byte("port = 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dsn := cctest.DSN(t)
	t.Setenv("CC_DATABASE_URL", dsn)
	seed(t, dsn, func(db *store.Store) error {
		repo := store.Repo{
			Name: sandboxRepo, Remote: filepath.Join(root, "remote.git"), State: store.RepoReady, TrackedAt: time.Now(),
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
	db, err := store.OpenStore(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	if err := write(db); err != nil {
		t.Fatal(err)
	}
}

// TestRunReturnsNilOnACleanShutdown pins the exit status of Ctrl-C. main calls log.Fatalf on any
// non-nil error from Run, so a shutdown path that reports failure turns every normal stop into
// exit status 1.
func TestRunReturnsNilOnACleanShutdown(t *testing.T) {
	configPath := appConfig(t)

	stub := func(context.Context) (plan.Observation, error) { return plan.Observation{}, nil }
	ctx, cancel := context.WithCancel(t.Context())
	inst, err := app.New(ctx, configPath, app.WithObserver(stub))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = inst.Close() })

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

type frozenClock struct{ at time.Time }

func (c frozenClock) Now() time.Time                       { return c.at }
func (frozenClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

func fixedClock(at time.Time) loop.Clock { return frozenClock{at} }

func TestNewStartsWithNoTrackedRepos(t *testing.T) {
	t.Setenv("CC_DATA_DIR", t.TempDir())
	t.Setenv("CC_DATABASE_URL", cctest.DSN(t))
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	inst, err := app.New(t.Context(), configPath)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = inst.Close() })

	if err := inst.RunOnce(t.Context()); err != nil {
		t.Errorf("RunOnce with no repos = %v, want nil", err)
	}
}
