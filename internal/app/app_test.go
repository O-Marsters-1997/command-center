package app_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
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
		PRs: map[string]plan.PR{"cc-sandbox//cc-1-first": {Number: 41, State: plan.Open}},
	}
	stub := func(context.Context) (plan.Observation, error) { return observed, nil }

	ctx := t.Context()
	inst, err := app.New(ctx, configPath, app.WithClock(fixedClock(at)), app.WithObserver(stub), stubSquashOnly)
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

	rec := httptest.NewRecorder()
	inst.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
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
	first, err := app.New(ctx, configPath, stubSquashOnly)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = first.Close() })

	if _, err := app.New(ctx, configPath, stubSquashOnly); err == nil {
		t.Fatal("a second instance started against the same workspace")
	}
}

// appConfig writes a repo-only config beside a real checkout of the repo it names and seeds two
// tickets straight into the workspace's database: the loop's reconcile doesn't care whether a
// row arrived by import or was seeded directly, so this skips the tracker entirely.
func appConfig(t *testing.T) string {
	t.Helper()
	dataDir := t.TempDir()
	t.Setenv("CC_DATA_DIR", dataDir)

	root, repoPath := repoWithOrigin(t)
	if err := os.Rename(repoPath, filepath.Join(root, "cc-sandbox")); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "command-centre.toml")
	body := "port = 0\n[[repo]]\nname = \"cc-sandbox\"\npath = \"cc-sandbox\"\n"
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	dsn := cctest.DSN(t)
	t.Setenv("CC_DATABASE_URL", dsn)
	seedTickets(t, dsn)
	return configPath
}

func seedTickets(t *testing.T, dsn string) {
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
	err = db.UpsertTickets(t.Context(), []store.Ticket{
		{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"},
		{URL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2-second", BlockedBy: []string{"sandbox://CC-1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
}

// stubSquashOnly stands in for the real gh-backed check, which these tests must not shell out
// to: none of their fixture repos are real git checkouts with a GitHub remote.
var stubSquashOnly = app.WithRepoCheck(func(context.Context, []config.Repo) error { return nil })

func TestNewRefusesARepoThatAllowsMergeCommits(t *testing.T) {
	configPath := appConfig(t)

	notSquashOnly := app.WithRepoCheck(func(_ context.Context, repos []config.Repo) error {
		return fmt.Errorf("repo %s allows merge commits (allow_merge_commit=true): "+
			"command-centre requires squash-only merges, refusing to start", repos[0].Name)
	})

	_, err := app.New(t.Context(), configPath, notSquashOnly)
	if err == nil {
		t.Fatal("New started despite a repo that allows merge commits")
	}
	if !strings.Contains(err.Error(), "cc-sandbox") || !strings.Contains(err.Error(), "allow_merge_commit") {
		t.Errorf("error %q does not name the offending repo and setting", err)
	}
}

// TestNewClonesARemoteRepoIntoAnEmptyDataDir: a config naming only a remote, and an empty data
// directory, reach a serving state with no directory prepared by hand.
func TestNewClonesARemoteRepoIntoAnEmptyDataDir(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("CC_DATA_DIR", dataDir)
	t.Setenv("CC_DATABASE_URL", cctest.DSN(t))

	_, repoPath := repoWithOrigin(t)
	remote := filepath.Join(filepath.Dir(repoPath), "remote.git")

	configPath := filepath.Join(t.TempDir(), "config.toml")
	body := "[[repo]]\nname = \"cc-sandbox\"\nremote = " + strconv.Quote(remote) + "\n"
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	stub := func(context.Context) (plan.Observation, error) { return plan.Observation{}, nil }
	inst, err := app.New(t.Context(), configPath, app.WithObserver(stub), stubSquashOnly)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = inst.Close() })

	checkout := filepath.Join(dataDir, "repos", "cc-sandbox")
	if _, err := os.Stat(filepath.Join(checkout, "README.md")); err != nil {
		t.Fatalf("startup did not clone into %s: %v", checkout, err)
	}

	rec := httptest.NewRecorder()
	inst.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// TestRunReturnsNilOnACleanShutdown pins the exit status of Ctrl-C. main calls log.Fatalf on any
// non-nil error from Run, so a shutdown path that reports failure turns every normal stop into
// exit status 1.
func TestRunReturnsNilOnACleanShutdown(t *testing.T) {
	configPath := appConfig(t)

	stub := func(context.Context) (plan.Observation, error) { return plan.Observation{}, nil }
	ctx, cancel := context.WithCancel(t.Context())
	inst, err := app.New(ctx, configPath, app.WithObserver(stub), stubSquashOnly)
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
