package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
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

func TestReadRepoSettingsAnswersDefaultsWhenTheFileIsMissing(t *testing.T) {
	t.Parallel()

	got, source, err := config.ReadRepoSettings(t.Context(), originedRepo(t, ""))
	if err != nil {
		t.Fatalf("ReadRepoSettings: %v", err)
	}
	if source != config.SourceDefaults {
		t.Errorf("source = %q, want %q", source, config.SourceDefaults)
	}
	if diff := cmp.Diff(config.RepoSettings{Tracker: "github"}, got); diff != "" {
		t.Errorf("ReadRepoSettings() mismatch (-want +got):\n%s", diff)
	}
}

func TestReadRepoSettingsDecodesEveryKey(t *testing.T) {
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
	got, source, err := config.ReadRepoSettings(t.Context(), originedRepo(t, body))
	if err != nil {
		t.Fatalf("ReadRepoSettings: %v", err)
	}
	want := config.RepoSettings{
		Tracker: "linear", Stacking: true, CompatCheck: "compat", MergifySHA: "sha256:1",
		Deny: []string{"go.mod"}, VerifyCommand: []string{"go", "vet"},
		Checks: verdict.Predicate{AllOf: []verdict.Predicate{
			{Success: "Lint"},
			{AnyOf: []verdict.Predicate{{Success: "A"}, {Author: "bot"}}},
		}},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("ReadRepoSettings() mismatch (-want +got):\n%s", diff)
	}
	if source != config.SourceFile {
		t.Errorf("source = %q, want %q", source, config.SourceFile)
	}
}

func TestReadRepoSettingsIgnoresABranchAndTheWorkingTree(t *testing.T) {
	t.Parallel()

	checkout := originedRepo(t, "deny = [\"go.mod\"]\n")
	gittest.Run(t, checkout, "switch", "-q", "-c", "ticket")
	writeIn(t, checkout, config.SettingsFile, "deny = []\n")
	gittest.Run(t, checkout, "commit", "-q", "-am", "loosen")
	writeIn(t, checkout, config.SettingsFile, "deny = []\nstacking = true\n")

	got, _, err := config.ReadRepoSettings(t.Context(), checkout)
	if err != nil {
		t.Fatalf("ReadRepoSettings: %v", err)
	}
	if !slices.Equal(got.Deny, []string{"go.mod"}) || got.Stacking {
		t.Errorf("settings = %+v, want origin/main's file alone", got)
	}
}

func TestReadRepoSettingsErrorsNameTheLine(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, body string
		wantLine   int
	}{
		{"parse error", "stacking = true\ndeny = [\n", 2},
		{"unknown key", "stacking = true\nstackin = true\n", 2},
		{"invalid predicate", "stacking = true\n[checks]\nsuccess = \"a\"\nskipped = \"b\"\n", 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, _, err := config.ReadRepoSettings(t.Context(), originedRepo(t, tt.body))
			var parseErr *config.SettingsParseError
			if !errors.As(err, &parseErr) || parseErr.Line != tt.wantLine {
				t.Errorf("ReadRepoSettings error = %v, want line %d", err, tt.wantLine)
			}
		})
	}
}
