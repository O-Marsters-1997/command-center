package cc_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
)

// fakeRunner drives loop orchestration tests without touching the OS: Spawn hands out
// sequential pids, Liveness and Reap answer from maps the test controls directly.
type fakeRunner struct {
	spawns   []cc.SpawnConfig
	nextPid  int
	alive    map[int]bool
	reapCode map[int]int
	canReap  map[int]bool
	failNext bool
	canceled []int
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{alive: map[int]bool{}, reapCode: map[int]int{}, canReap: map[int]bool{}}
}

func (f *fakeRunner) Spawn(_ context.Context, cfg cc.SpawnConfig) (cc.SpawnResult, error) {
	f.spawns = append(f.spawns, cfg)
	if f.failNext {
		f.failNext = false
		return cc.SpawnResult{}, errSpawnFailed
	}
	f.nextPid++
	f.alive[f.nextPid] = true
	return cc.SpawnResult{Pid: f.nextPid}, nil
}

func (f *fakeRunner) Liveness(pgid int, _, _ time.Time) (bool, error) {
	return f.alive[pgid], nil
}

func (f *fakeRunner) Cancel(pgid int) error {
	f.alive[pgid] = false
	f.canceled = append(f.canceled, pgid)
	return nil
}

func (f *fakeRunner) Reap(pid int) (int, bool) {
	if !f.canReap[pid] {
		return 0, false
	}
	return f.reapCode[pid], true
}

var errSpawnFailed = &spawnError{}

type spawnError struct{}

func (*spawnError) Error() string { return "spawn failed" }

// installFakeTp puts a script named tp on PATH that delegates to real git worktree add, so
// internal/tp.New is genuinely exercised. exitCode non-zero simulates `tp new` failing
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
	repoPath = filepath.Join(root, "repo")
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

func testConfigAndWorkspace(t *testing.T, root string, maxAgents int, agentCommand []string) (cc.Config, cc.Workspace) {
	t.Helper()
	cfg := cc.Config{
		MaxAgents:    maxAgents,
		AgentCommand: agentCommand,
		Repos:        []cc.Repo{{Name: "repo", Checkout: filepath.Join(root, "repo"), Stacking: false}},
	}
	ws := cc.Workspace{
		RunsDir:      t.TempDir(),
		SettingsPath: filepath.Join(t.TempDir(), "agent.json"),
	}
	return cfg, ws
}
