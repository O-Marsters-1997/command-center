package cc_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
	"github.com/O-Marsters-1997/command-center/internal/gh"
)

func TestNewRunsATickAndServesThePage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	root := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, ".claude", "command-centre.toml")
	if err := os.WriteFile(configPath, []byte(twoTasks), 0o600); err != nil {
		t.Fatal(err)
	}

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
	// The config's tasks were upserted at startup and both rows derive from the stub's snapshot:
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
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	root := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, ".claude", "command-centre.toml")
	if err := os.WriteFile(configPath, []byte(twoTasks), 0o600); err != nil {
		t.Fatal(err)
	}

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

// stubSquashOnly stands in for the real gh-backed check, which these tests must not shell out
// to: none of their fixture repos are real git checkouts with a GitHub remote.
var stubSquashOnly = cc.WithRepoCheck(func(context.Context, cc.Workspace, []cc.Repo) error { return nil })

func TestNewRefusesARepoThatAllowsMergeCommits(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	root := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, ".claude", "command-centre.toml")
	if err := os.WriteFile(configPath, []byte(twoTasks), 0o600); err != nil {
		t.Fatal(err)
	}

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
