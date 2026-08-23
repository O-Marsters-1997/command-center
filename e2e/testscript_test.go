//go:build e2e

package e2e_test

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rogpeppe/go-internal/testscript"
)

// testdataDir is absolute because the harness commands run with the script's work directory as
// their cwd, not the package's.
var testdataDir string

// agentsDir is the repo-root testdata/agents directory, holding the fake agent scripts a
// config fixture's agent_command points at. Absolute for the same reason as testdataDir.
var agentsDir string

func TestMain(m *testing.M) {
	os.Exit(runScripts(m))
}

// runScripts builds the binaries the scripts exec and puts them first on PATH. It is separate
// from TestMain so the deferred cleanup runs before os.Exit.
func runScripts(m *testing.M) (code int) {
	abs, err := filepath.Abs("testdata")
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e: %v\n", err)
		return 1
	}
	testdataDir = abs

	agentsAbs, err := filepath.Abs("../testdata/agents")
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e: %v\n", err)
		return 1
	}
	agentsDir = agentsAbs

	bin, err := os.MkdirTemp("", "cc-e2e-bin")
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e: %v\n", err)
		return 1
	}
	defer func() { _ = os.RemoveAll(bin) }()

	// The fakes are installed under the names of the tools they stand in for: putting this
	// directory first on PATH is the whole substitution mechanism.
	if err := errors.Join(
		build(bin, "cc", "./cmd/cc", "-tags=e2e"),
		build(bin, "gh", "./e2e/fakegh"),
		build(bin, "tp", "./e2e/faketp"),
	); err != nil {
		fmt.Fprintf(os.Stderr, "e2e: %v\n", err)
		return 1
	}

	if err := os.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH")); err != nil {
		fmt.Fprintf(os.Stderr, "e2e: %v\n", err)
		return 1
	}
	return m.Run()
}

// build compiles pkg into dir under the given name. The module root is one level up from the
// package directory the test binary runs in.
func build(dir, name, pkg string, flags ...string) error {
	args := append([]string{"build"}, flags...)
	cmd := exec.Command("go", append(args, "-o", filepath.Join(dir, name), pkg)...)
	cmd.Dir = ".."
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("building %s: %w\n%s", name, err, out)
	}
	return nil
}

func TestScripts(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Dir:   "tests",
		Setup: setup,
		Cmds: map[string]func(*testscript.TestScript, bool, []string){
			"cc-init-repo":    ccInitRepo,
			"cc-config":       ccConfig,
			"cc-fake-gh":      ccFakeGh,
			"cc-fake-gh-head": ccFakeGhHead,
			"cc-daemon":       ccDaemon,
		},
		RequireExplicitExec: true,
		RequireUniqueNames:  true,
	})
}

// ccInitRepo builds the repository the config points at, plus the bare origin it fetches from.
// Observe's first act is `git fetch origin --prune`, so without a remote every script
// fail-closes before it reaches its assertion.
func ccInitRepo(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) != 0 {
		ts.Fatalf("usage: cc-init-repo")
	}
	work := ts.Getenv("WORK")
	repo := filepath.Join(work, "repo")
	remote := filepath.Join(work, "remote.git")

	ts.Check(ts.Exec("git", "init", "-q", "-b", "main", repo))
	ts.Check(os.WriteFile(filepath.Join(repo, "README.md"), []byte("e2e\n"), 0o600))
	ts.Check(ts.Exec("git", "-C", repo, "add", "README.md"))
	ts.Check(ts.Exec("git", "-C", repo, "commit", "-q", "-m", "initial commit"))
	ts.Check(ts.Exec("git", "init", "-q", "--bare", remote))
	ts.Check(ts.Exec("git", "-C", repo, "remote", "add", "origin", remote))
	ts.Check(ts.Exec("git", "-C", repo, "push", "-q", "-u", "origin", "main"))
}

// ccConfig installs a named config fixture at cc's default path, so no script ever passes
// --config. Port 0 is forced rather than left to the fixture: scripts run in parallel and a
// fixed port collides. {{agents}} is substituted with agentsDir so a fixture's agent_command can
// point at a fake agent script without hardcoding this checkout's absolute path.
func ccConfig(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) != 1 {
		ts.Fatalf("usage: cc-config <name>")
	}
	body, err := os.ReadFile(filepath.Join(testdataDir, "config", args[0]+".toml"))
	ts.Check(err)
	body = []byte(strings.ReplaceAll(string(body), "{{agents}}", agentsDir))

	dir := filepath.Join(ts.Getenv("WORK"), ".claude")
	ts.Check(os.MkdirAll(dir, 0o700))
	ts.Check(os.WriteFile(filepath.Join(dir, "command-centre.toml"), append([]byte("port = 0\n"), body...), 0o600))
}

// ccFakeGh stages the fixture the fake gh answers from. It is read at exec time, so a script
// may restage it between ticks.
func ccFakeGh(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) != 1 {
		ts.Fatalf("usage: cc-fake-gh <name>")
	}
	body, err := os.ReadFile(filepath.Join(testdataDir, "gh", args[0]+".json"))
	ts.Check(err)
	ts.Check(os.WriteFile(ts.Getenv("CC_GH_FIXTURE"), body, 0o600))
}

// ccFakeGhHead stages a gh fixture template whose {{head}} placeholder is replaced with the real
// current tip of a branch in the repo -- the verdict scripts' way of pointing a fixture's
// headRefOid at a commit whose SHA a script cannot know ahead of time.
func ccFakeGhHead(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) != 2 {
		ts.Fatalf("usage: cc-fake-gh-head <fixture> <branch>")
	}
	work := ts.Getenv("WORK")
	out, err := exec.Command("git", "-C", filepath.Join(work, "repo"), "rev-parse", args[1]).Output()
	ts.Check(err)
	head := strings.TrimSpace(string(out))

	body, err := os.ReadFile(filepath.Join(testdataDir, "gh", args[0]+".json"))
	ts.Check(err)
	body = []byte(strings.ReplaceAll(string(body), "{{head}}", head))
	ts.Check(os.WriteFile(ts.Getenv("CC_GH_FIXTURE"), body, 0o600))
}

// daemonReady is the line cc logs once it holds the flock and is about to serve.
const daemonReady = "serving http://"

// ccDaemon starts a real cc in the background and returns only once it is holding the flock.
// The readiness poll is the point: sleeping instead would make the second-instance assertion a
// race, and a flaky test at the bottom of a stack is the worst place for one.
func ccDaemon(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) != 0 {
		ts.Fatalf("usage: cc-daemon")
	}
	work := ts.Getenv("WORK")
	logPath := filepath.Join(work, "cc-daemon.log")
	logFile, err := os.Create(logPath)
	ts.Check(err)

	daemon := exec.Command("cc")
	daemon.Dir = work
	daemon.Env = append(scriptEnv(work), "PATH="+ts.Getenv("PATH"), "TMPDIR="+ts.Getenv("TMPDIR"))
	daemon.Stdout = logFile
	daemon.Stderr = logFile
	ts.Check(daemon.Start())
	ts.Defer(func() {
		_ = daemon.Process.Kill()
		_ = daemon.Wait()
		_ = logFile.Close()
	})

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if body, err := os.ReadFile(logPath); err == nil && strings.Contains(string(body), daemonReady) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	ts.Fatalf("cc did not log %q within the deadline:\n%s", daemonReady, ts.ReadFile(logPath))
}

func setup(env *testscript.Env) error {
	env.Vars = append(env.Vars, scriptEnv(env.WorkDir)...)
	return nil
}

// scriptEnv is the environment a script and anything it starts run under. HOME is the work
// directory because os.UserConfigDir honours it: that alone redirects the state dir, so no
// test-only environment variable has to exist in production code.
//
// The git identity is passed by environment, and the system config suppressed, so cc-init-repo
// can commit on a CI runner with no gitconfig at all.
func scriptEnv(work string) []string {
	return []string{
		"HOME=" + work,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=Command Centre",
		"GIT_AUTHOR_EMAIL=cc@example.com",
		"GIT_COMMITTER_NAME=Command Centre",
		"GIT_COMMITTER_EMAIL=cc@example.com",
		"CC_GH_FIXTURE=" + filepath.Join(work, "gh-fixture.json"),
		"CC_GH_LOG=" + filepath.Join(work, "gh.log"),
		"CC_TP_LOG=" + filepath.Join(work, "tp.log"),
		// Read by testdata/agents/commits.sh and empty.sh, inherited by every spawned agent
		// process (cc.ProcessRunner.Spawn never strips it) -- a script's proof of exactly how
		// many times an agent actually ran, distinct from tp.log's cuts or events' own count.
		"CC_AGENT_LOG=" + filepath.Join(work, "agent.log"),
	}
}
