package loop_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

func pathWithFakeGh(t *testing.T, apiJSON string) {
	t.Helper()
	bin := t.TempDir()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("LookPath(git): %v", err)
	}
	if err := os.Symlink(git, filepath.Join(bin, "git")); err != nil {
		t.Fatalf("Symlink(git): %v", err)
	}
	script := "#!/bin/sh\nprintf '%s' '" + apiJSON + "'\n"
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(gh): %v", err)
	}
	t.Setenv("PATH", bin)
}

const squashOnlyMain = `{"allow_merge_commit":false,"allow_rebase_merge":false,"default_branch":"main"}`

func TestValidateRepo(t *testing.T) {
	t.Run("refuses a master default before cloning it", func(t *testing.T) {
		pathWithFakeGh(t, `{"allow_merge_commit":false,"allow_rebase_merge":false,"default_branch":"master"}`)
		dataDir := t.TempDir()
		repo := storepkg.Repo{Name: "acme/old", Remote: "/nowhere/remote.git", Phase: plan.RepoCloning}

		got, err := loop.ValidateRepo(t.Context(), dataDir, repo, testAt)
		if err != nil {
			t.Fatalf("ValidateRepo() error = %v", err)
		}
		if got.Phase != plan.RepoRefused || got.RefusalKind != plan.RefusalDefaultBranch {
			t.Errorf("ValidateRepo(acme/old) = %+v, want refused(default_branch)", got)
		}
		if _, err := os.Stat(config.CheckoutPath(dataDir, repo.Name)); !os.IsNotExist(err) {
			t.Errorf("Stat(checkout) error = %v, want it never cloned", err)
		}
	})

	t.Run("clones an accepted repo and reads its settings", func(t *testing.T) {
		root, _ := repoWithOrigin(t)
		pathWithFakeGh(t, squashOnlyMain)
		dataDir := t.TempDir()
		repo := storepkg.Repo{Name: "acme/cc", Remote: filepath.Join(root, "remote.git"), Phase: plan.RepoCloning}

		got, err := loop.ValidateRepo(t.Context(), dataDir, repo, testAt)
		if err != nil {
			t.Fatalf("ValidateRepo() error = %v", err)
		}
		if got.Phase != plan.RepoReady || got.SettingsSource != string(config.SourceDefaults) ||
			!got.SettingsReadAt.Equal(testAt) {
			t.Errorf("ValidateRepo(acme/cc) = %+v, want ready from defaults at %v", got, testAt)
		}
		if _, err := os.Stat(filepath.Join(config.CheckoutPath(dataDir, repo.Name), "README.md")); err != nil {
			t.Errorf("Stat(cloned README) error = %v", err)
		}
	})

	t.Run("refuses a repo whose settings file does not parse", func(t *testing.T) {
		root, repoPath := repoWithOrigin(t)
		pushSettingsFile(t, repoPath, "stackin = true\n")
		pathWithFakeGh(t, squashOnlyMain)
		repo := storepkg.Repo{Name: "acme/cc", Remote: filepath.Join(root, "remote.git"), Phase: plan.RepoCloning}

		got, err := loop.ValidateRepo(t.Context(), t.TempDir(), repo, testAt)
		if err != nil {
			t.Fatalf("ValidateRepo() error = %v", err)
		}
		if got.Phase != plan.RepoRefused || got.RefusalKind != plan.RefusalSettingsParse {
			t.Errorf("ValidateRepo(acme/cc) = %+v, want refused(settings_parse)", got)
		}
	})
}
