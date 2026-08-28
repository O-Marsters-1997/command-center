package cc_test

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

	"github.com/O-Marsters-1997/command-center/internal/cc"
	"github.com/O-Marsters-1997/command-center/internal/gh"
)

func TestNewRunsATickAndServesThePage(t *testing.T) {
	configPath := appConfig(t)

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	observed := cc.Observation{PRs: map[string]gh.PR{"cc-1-first": {Number: 41, State: gh.Open}}}
	stub := func(context.Context) (cc.Observation, error) { return observed, nil }

	ctx := t.Context()
	app, err := cc.New(ctx, configPath, cc.WithClock(fixedClock(at)), cc.WithObserver(stub), stubSquashOnly)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		if err := app.Close(); err != nil {
			t.Errorf("close app: %v", err)
		}
	})

	if err := app.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	body := rec.Body.String()
	// The config's tickets were upserted at startup and both rows derive from the stub's snapshot:
	// CC-1 has no blockers, CC-2's blocker now has an open PR.
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
	first, err := cc.New(ctx, configPath, stubSquashOnly)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = first.Close() })

	if _, err := cc.New(ctx, configPath, stubSquashOnly); err == nil {
		t.Fatal("a second instance started against the same workspace")
	}
}

// appConfig writes twoTickets beside a real checkout of the repo it names, and points CC_DATA_DIR
// at an empty directory, so cc.New's startup checkout and workspace both resolve.
func appConfig(t *testing.T) string {
	t.Helper()
	t.Setenv("CC_DATA_DIR", t.TempDir())

	root, repoPath := repoWithOrigin(t)
	if err := os.Rename(repoPath, filepath.Join(root, "cc-sandbox")); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "command-centre.toml")
	if err := os.WriteFile(configPath, []byte(twoTickets), 0o600); err != nil {
		t.Fatal(err)
	}
	return configPath
}

// stubSquashOnly stands in for the real gh-backed check, which these tests must not shell out
// to: none of their fixture repos are real git checkouts with a GitHub remote.
var stubSquashOnly = cc.WithRepoCheck(func(context.Context, cc.Workspace, []cc.Repo) error { return nil })

func TestNewRefusesARepoThatAllowsMergeCommits(t *testing.T) {
	configPath := appConfig(t)

	notSquashOnly := cc.WithRepoCheck(func(_ context.Context, _ cc.Workspace, repos []cc.Repo) error {
		return fmt.Errorf("repo %s allows merge commits (allow_merge_commit=true): "+
			"command-centre requires squash-only merges, refusing to start", repos[0].Name)
	})

	_, err := cc.New(t.Context(), configPath, notSquashOnly)
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

	_, repoPath := repoWithOrigin(t)
	remote := filepath.Join(filepath.Dir(repoPath), "remote.git")

	configPath := filepath.Join(t.TempDir(), "config.toml")
	body := "[[repo]]\nname = \"cc-sandbox\"\nremote = " + strconv.Quote(remote) + "\n"
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	stub := func(context.Context) (cc.Observation, error) { return cc.Observation{}, nil }
	app, err := cc.New(t.Context(), configPath, cc.WithObserver(stub), stubSquashOnly)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = app.Close() })

	checkout := filepath.Join(dataDir, "repos", "cc-sandbox")
	if _, err := os.Stat(filepath.Join(checkout, "README.md")); err != nil {
		t.Fatalf("startup did not clone into %s: %v", checkout, err)
	}

	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}
