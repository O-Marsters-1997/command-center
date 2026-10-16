package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/gittest"
	"github.com/O-Marsters-1997/command-center/internal/verdict"
)

func originedRepo(t *testing.T, settings string) string {
	t.Helper()
	checkout, _ := gittest.WithOrigin(t)
	if settings != "" {
		gittest.Commit(t, checkout, config.SettingsFile, settings)
		gittest.Run(t, checkout, "push", "-q", "origin", "main")
	}
	return checkout
}

func writeIn(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile(%s): %v", filepath.Join(dir, name), err)
	}
}

func TestReadRepoSettings(t *testing.T) {
	t.Parallel()

	type result struct {
		Settings config.RepoSettings
		Source   config.Source
	}

	t.Run("a missing file answers defaults", func(t *testing.T) {
		t.Parallel()
		settings, source, err := config.ReadRepoSettings(t.Context(), originedRepo(t, ""))
		if err != nil {
			t.Fatalf("ReadRepoSettings() error = %v", err)
		}
		want := result{Settings: config.RepoSettings{Tracker: "github"}, Source: config.SourceDefaults}
		if diff := cmp.Diff(want, result{settings, source}); diff != "" {
			t.Errorf("ReadRepoSettings() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("decodes every key", func(t *testing.T) {
		t.Parallel()
		body := `
tracker        = "linear"
stacking       = true
compat_check   = "compat"
mergify_sha    = "sha256:1"
deny           = ["go.mod"]
verify_command = ["go", "vet"]

[checks]
all_of = [{ success = "Lint" }, { any_of = [{ success = "A" }, { author = "bot" }] }]
`
		settings, source, err := config.ReadRepoSettings(t.Context(), originedRepo(t, body))
		if err != nil {
			t.Fatalf("ReadRepoSettings() error = %v", err)
		}
		want := result{
			Settings: config.RepoSettings{
				Tracker: "linear", Stacking: true, CompatCheck: "compat", MergifySHA: "sha256:1",
				Deny: []string{"go.mod"}, VerifyCommand: []string{"go", "vet"},
				Checks: verdict.Predicate{AllOf: []verdict.Predicate{
					{Success: "Lint"},
					{AnyOf: []verdict.Predicate{{Success: "A"}, {Author: "bot"}}},
				}},
			},
			Source: config.SourceFile,
		}
		if diff := cmp.Diff(want, result{settings, source}); diff != "" {
			t.Errorf("ReadRepoSettings() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("reads origin/main, never a branch or the working tree", func(t *testing.T) {
		t.Parallel()
		checkout := originedRepo(t, "deny = [\"go.mod\"]\n")
		gittest.Run(t, checkout, "switch", "-q", "-c", "ticket")
		writeIn(t, checkout, config.SettingsFile, "deny = []\n")
		gittest.Run(t, checkout, "commit", "-q", "-am", "loosen")
		writeIn(t, checkout, config.SettingsFile, "deny = []\nstacking = true\n")

		settings, _, err := config.ReadRepoSettings(t.Context(), checkout)
		if err != nil {
			t.Fatalf("ReadRepoSettings() error = %v", err)
		}
		want := config.RepoSettings{Tracker: "github", Deny: []string{"go.mod"}}
		if diff := cmp.Diff(want, settings); diff != "" {
			t.Errorf("ReadRepoSettings() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("errors name the line", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name, body string
			wantLine   int
		}{
			{"parse error", "stacking = true\ndeny = [\n", 2},
			{"unknown key", "stacking = true\nstackin = true\n", 2},
			{"invalid predicate", "stacking = true\n[checks]\nsuccess = \"a\"\nskipped = \"b\"\n", 2},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				_, _, err := config.ReadRepoSettings(t.Context(), originedRepo(t, tt.body))
				parseErr, ok := errors.AsType[*config.SettingsParseError](err)
				if !ok || parseErr.Line != tt.wantLine {
					t.Errorf("ReadRepoSettings(%q) error = %v, want a parse error on line %d", tt.body, err, tt.wantLine)
				}
			})
		}
	})
}
