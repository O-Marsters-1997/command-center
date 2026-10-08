package config_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/verdict"
)

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func originedRepo(t *testing.T, settings string) string {
	t.Helper()
	root := t.TempDir()
	checkout := filepath.Join(root, "repo")
	remote := filepath.Join(root, "remote.git")
	gitIn(t, root, "init", "-q", "-b", "main", checkout)
	gitIn(t, root, "init", "-q", "-b", "main", "--bare", remote)
	writeIn(t, checkout, "README.md", "hi\n")
	if settings != "" {
		writeIn(t, checkout, config.SettingsFile, settings)
	}
	gitIn(t, checkout, "add", ".")
	gitIn(t, checkout, "commit", "-q", "-m", "initial")
	gitIn(t, checkout, "remote", "add", "origin", remote)
	gitIn(t, checkout, "push", "-q", "-u", "origin", "main")
	gitIn(t, checkout, "fetch", "-q", "origin")
	return checkout
}

func writeIn(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
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
	sameSettings(t, config.RepoSettings{Tracker: "github"}, got)
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
	sameSettings(t, want, got)
	if source != config.SourceFile {
		t.Errorf("source = %q, want %q", source, config.SourceFile)
	}
}

func TestReadRepoSettingsIgnoresABranchAndTheWorkingTree(t *testing.T) {
	t.Parallel()

	checkout := originedRepo(t, "deny = [\"go.mod\"]\n")
	gitIn(t, checkout, "switch", "-q", "-c", "ticket")
	writeIn(t, checkout, config.SettingsFile, "deny = []\n")
	gitIn(t, checkout, "commit", "-q", "-am", "loosen")
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
		name, body, wantLine string
	}{
		{"parse error", "stacking = true\ndeny = [\n", "line 2"},
		{"unknown key", "stacking = true\nstackin = true\n", "line 2"},
		{"invalid predicate", "stacking = true\n[checks]\nsuccess = \"a\"\nskipped = \"b\"\n", "line 2"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, _, err := config.ReadRepoSettings(t.Context(), originedRepo(t, tt.body))
			if err == nil || !strings.Contains(err.Error(), tt.wantLine) {
				t.Errorf("err = %v, want one naming %s", err, tt.wantLine)
			}
		})
	}
}

func TestTheCommittedSettingsFileHoldsWhatTheRepoBlockHeld(t *testing.T) {
	t.Parallel()

	body, err := os.ReadFile("../../" + config.SettingsFile)
	if err != nil {
		t.Fatal(err)
	}
	got, err := config.ParseRepoSettings(string(body))
	if err != nil {
		t.Fatalf("ParseRepoSettings: %v", err)
	}

	var all []verdict.Predicate
	for _, name := range []string{
		"test", "e2e", "lint", "build (ubuntu-latest)", "build (macos-latest)", "tidy", "assets",
	} {
		all = append(all, verdict.Predicate{Success: name})
	}
	want := config.RepoSettings{
		Tracker:  "github",
		Stacking: true,
		Deny:     []string{".github/**", "go.mod", "go.sum"},
		Checks:   verdict.Predicate{AllOf: all},
	}
	sameSettings(t, want, got)
}

func sameSettings(t *testing.T, want, got config.RepoSettings) {
	t.Helper()
	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(wantJSON) != string(gotJSON) {
		t.Errorf("settings = %s, want %s", gotJSON, wantJSON)
	}
}
