package loop_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/cctest"
	"github.com/O-Marsters-1997/command-center/internal/config"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

func openStore(t *testing.T) *storepkg.Store {
	t.Helper()
	return openStoreAt(t, cctest.DSN(t))
}

func openStoreAt(t *testing.T, dsn string) *storepkg.Store {
	t.Helper()
	store, err := storepkg.OpenStore(t.Context(), dsn)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})
	return store
}

// installFakeTp puts a script named tp on PATH that delegates to real git worktree add, so
// internal/git.CLI.New is genuinely exercised. exitCode non-zero simulates `tp new` failing
// (an unresolvable base), matching faketp's own $CC_TP_FAIL behaviour.
func installFakeTp(t *testing.T, fail bool) {
	t.Helper()
	bin := t.TempDir()
	var script string
	if fail {
		script = "#!/bin/sh\necho 'faketp: forced failure' >&2\nexit 1\n"
	} else {
		// argv is `tp new <branch> --base <baseRef>`: $1 is the subcommand, $2 the branch,
		// $4 the base ref.
		script = "#!/bin/sh\n" +
			"set -eu\n" +
			"branch=\"$2\"\n" +
			"base=\"$4\"\n" +
			"path=\"$(cd \"$(dirname \"$PWD\")\" && pwd)/wt-$branch\"\n" +
			"git worktree add -b \"$branch\" \"$path\" \"$base\" >&2\n" +
			"printf '%s\\n' \"$path\"\n"
	}
	if err := os.WriteFile(filepath.Join(bin, "tp"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// repoWithOrigin creates a real repo with a pushed origin/main, the shape tp new --base
// origin/<branch> needs to resolve against.
func repoWithOrigin(t *testing.T) (root, repoPath string) {
	t.Helper()
	root = t.TempDir()
	repoPath = config.CheckoutPath(root, "repo")
	remote := filepath.Join(root, "remote.git")

	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.com")
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Env = os.Environ()
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main", repoPath)
	if err := os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("hi\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("-C", repoPath, "add", "README.md")
	run("-C", repoPath, "commit", "-q", "-m", "initial")
	run("init", "-q", "-b", "main", "--bare", remote)
	run("-C", repoPath, "remote", "add", "origin", remote)
	run("-C", repoPath, "push", "-q", "-u", "origin", "main")
	run("-C", repoPath, "fetch", "-q", "origin")
	return root, repoPath
}

func testConfigAndWorkspace(
	t *testing.T, root string, maxAgents int, agentCommand []string,
) (config.Config, config.Workspace) {
	t.Helper()
	cfg := config.Config{DataDir: root, MaxAgents: maxAgents, AgentCommand: agentCommand}
	ws := config.Workspace{
		RunsDir:      t.TempDir(),
		SettingsPath: filepath.Join(t.TempDir(), "agent.json"),
	}
	return cfg, ws
}

func pushSettingsFile(t *testing.T, repoPath, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repoPath, config.SettingsFile), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, "-C", repoPath, "add", config.SettingsFile)
	runGit(t, "-C", repoPath, "commit", "-q", "-m", "settings")
	runGit(t, "-C", repoPath, "push", "-q", "origin", "main")
}

func trackRepo(t *testing.T, store *storepkg.Store, name, remote string) {
	t.Helper()
	repo := storepkg.Repo{Name: name, Remote: remote, State: storepkg.RepoReady, TrackedAt: testAt}
	if err := store.UpsertRepo(t.Context(), repo); err != nil {
		t.Fatal(err)
	}
}
