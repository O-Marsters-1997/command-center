package config_test

import (
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/plan/verdict"
)

func TestReadRepoSettings(t *testing.T) {
	t.Parallel()

	type result struct {
		Settings config.RepoSettings
		Source   config.Source
	}

	t.Run("a missing file answers defaults", func(t *testing.T) {
		t.Parallel()
		settings, source, err := config.ReadRepoSettings(nil, false)
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
		settings, source, err := config.ReadRepoSettings([]byte(body), true)
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
				_, _, err := config.ReadRepoSettings([]byte(tt.body), true)
				parseErr, ok := errors.AsType[*config.SettingsParseError](err)
				if !ok || parseErr.Line != tt.wantLine {
					t.Errorf("ReadRepoSettings(%q) error = %v, want a parse error on line %d", tt.body, err, tt.wantLine)
				}
			})
		}
	})
}
