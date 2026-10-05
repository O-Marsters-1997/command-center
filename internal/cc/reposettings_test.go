package cc_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/cc"
)

func writeAndPushSettings(t *testing.T, repoPath, body string) {
	t.Helper()
	path := filepath.Join(repoPath, ".command-centre.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, "-C", repoPath, "add", ".command-centre.toml")
	runGit(t, "-C", repoPath, "commit", "-q", "-m", "add settings")
	runGit(t, "-C", repoPath, "push", "-q", "origin", "main")
	runGit(t, "-C", repoPath, "fetch", "-q", "origin")
}

func TestReadRepoSettingsAnswersDefaultsWhenTheFileIsMissing(t *testing.T) {
	_, repoPath := repoWithOrigin(t)

	settings, source, err := cc.ReadRepoSettings(t.Context(), repoPath)
	if err != nil {
		t.Fatalf("ReadRepoSettings: %v", err)
	}
	if source != cc.SettingsFromDefaults {
		t.Errorf("source = %q, want defaults", source)
	}
	if settings.Tracker != "github" {
		t.Errorf("tracker = %q, want default github", settings.Tracker)
	}
	if settings.Stacking || len(settings.Deny) != 0 {
		t.Errorf("settings = %+v, want every other field zero", settings)
	}
}

// TestReadRepoSettingsAnswersDefaultsWhenTheFileIsUncommitted covers git show's other "missing"
// phrasing: a working tree holding the file (as this checkout does, before this PR's own
// .command-centre.toml is merged) still reads as "nothing configured" rather than a refusal.
func TestReadRepoSettingsAnswersDefaultsWhenTheFileIsUncommitted(t *testing.T) {
	_, repoPath := repoWithOrigin(t)

	uncommitted := filepath.Join(repoPath, ".command-centre.toml")
	if err := os.WriteFile(uncommitted, []byte("deny = [\"scripts/gen.sh\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	settings, source, err := cc.ReadRepoSettings(t.Context(), repoPath)
	if err != nil {
		t.Fatalf("ReadRepoSettings: %v", err)
	}
	if source != cc.SettingsFromDefaults {
		t.Errorf("source = %q, want defaults", source)
	}
	if settings.Tracker != "github" || len(settings.Deny) != 0 {
		t.Errorf("settings = %+v, want defaults -- the uncommitted copy must not be read", settings)
	}
}

func TestReadRepoSettingsDecodesTheFileFromOriginMain(t *testing.T) {
	_, repoPath := repoWithOrigin(t)
	writeAndPushSettings(t, repoPath, `
tracker        = "linear"
stacking       = true
deny           = ["scripts/gen.sh"]
compat_check   = "GraphQL production compatibility"
mergify_sha    = "sha256:deadbeef"
verify_command = ["make", "verify"]
generated      = ["dist/**"]
build_command  = ["make", "dist"]

[checks]
success = "CI"
`)

	settings, source, err := cc.ReadRepoSettings(t.Context(), repoPath)
	if err != nil {
		t.Fatalf("ReadRepoSettings: %v", err)
	}
	if source != cc.SettingsFromFile {
		t.Errorf("source = %q, want file", source)
	}
	if settings.Tracker != "linear" {
		t.Errorf("tracker = %q, want linear", settings.Tracker)
	}
	if !settings.Stacking {
		t.Error("stacking = false, want true")
	}
	if len(settings.Deny) != 1 || settings.Deny[0] != "scripts/gen.sh" {
		t.Errorf("deny = %v", settings.Deny)
	}
	if settings.CompatCheck != "GraphQL production compatibility" {
		t.Errorf("compat_check = %q", settings.CompatCheck)
	}
	if settings.MergifySHA != "sha256:deadbeef" {
		t.Errorf("mergify_sha = %q", settings.MergifySHA)
	}
	if len(settings.VerifyCommand) != 2 {
		t.Errorf("verify_command = %v", settings.VerifyCommand)
	}
	if len(settings.Generated) != 1 || settings.Generated[0] != "dist/**" {
		t.Errorf("generated = %v", settings.Generated)
	}
	if len(settings.BuildCommand) != 2 {
		t.Errorf("build_command = %v", settings.BuildCommand)
	}
	if settings.Checks.Success != "CI" {
		t.Errorf("checks = %+v", settings.Checks)
	}
}

func TestReadRepoSettingsRefusesAnUnknownKey(t *testing.T) {
	_, repoPath := repoWithOrigin(t)
	writeAndPushSettings(t, repoPath, "stacking = true\npath = \"elsewhere\"\n")

	_, _, err := cc.ReadRepoSettings(t.Context(), repoPath)
	if err == nil || !strings.Contains(err.Error(), "path") {
		t.Errorf("ReadRepoSettings error = %v, want one naming the unknown key \"path\"", err)
	}
}

func TestReadRepoSettingsRefusesMalformedTOML(t *testing.T) {
	_, repoPath := repoWithOrigin(t)
	writeAndPushSettings(t, repoPath, "stacking = not-a-bool\n")

	_, _, err := cc.ReadRepoSettings(t.Context(), repoPath)
	if err == nil {
		t.Error("ReadRepoSettings = nil error, want a parse error")
	}
}

func TestReadRepoSettingsRefusesAnInvalidPredicate(t *testing.T) {
	_, repoPath := repoWithOrigin(t)
	writeAndPushSettings(t, repoPath, `
[checks]
success = "CI"
author  = "dependabot[bot]"
`)

	_, _, err := cc.ReadRepoSettings(t.Context(), repoPath)
	if err == nil || !strings.Contains(err.Error(), "checks") {
		t.Errorf("ReadRepoSettings error = %v, want one naming checks", err)
	}
}

// TestReadRepoSettingsIgnoresABranchsOwnFile covers the Scenario this ticket names: a branch
// cannot loosen the rules that bind it, because ReadRepoSettings only ever reads origin/main,
// never the branch its own ticket is cut from or the worktree it runs in.
func TestReadRepoSettingsIgnoresABranchsOwnFile(t *testing.T) {
	_, repoPath := repoWithOrigin(t)
	writeAndPushSettings(t, repoPath, "deny = [\"scripts/gen.sh\"]\n")

	runGit(t, "-C", repoPath, "checkout", "-q", "-b", "cc-1-loosen")
	looser := filepath.Join(repoPath, ".command-centre.toml")
	if err := os.WriteFile(looser, []byte("deny = []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, "-C", repoPath, "commit", "-q", "-am", "loosen deny on my own branch")
	runGit(t, "-C", repoPath, "push", "-q", "-u", "origin", "cc-1-loosen")

	settings, source, err := cc.ReadRepoSettings(t.Context(), repoPath)
	if err != nil {
		t.Fatalf("ReadRepoSettings: %v", err)
	}
	if source != cc.SettingsFromFile {
		t.Errorf("source = %q, want file (main's, not the branch's)", source)
	}
	if len(settings.Deny) != 1 || settings.Deny[0] != "scripts/gen.sh" {
		t.Errorf("deny = %v, want main's [scripts/gen.sh], not the branch's loosened list", settings.Deny)
	}
}
