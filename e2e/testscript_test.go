//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/rogpeppe/go-internal/testscript"

	"github.com/O-Marsters-1997/command-center/internal/cctest"
	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/git"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

var testdataDir string

var agentsDir string

func TestMain(m *testing.M) {
	os.Exit(runScripts(m))
}

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
	t.Parallel()
	testscript.Run(t, testscript.Params{
		Dir:   "tests",
		Setup: setup,
		Cmds: map[string]func(*testscript.TestScript, bool, []string){
			"cc-init-repo":    ccInitRepo,
			"cc-config":       ccConfig,
			"cc-fake-gh":      ccFakeGh,
			"cc-fake-gh-head": ccFakeGhHead,
			"cc-daemon":       ccDaemon,
			"cc-wait":         ccWait,
		},
		RequireExplicitExec: true,
		RequireUniqueNames:  true,
	})
}

func TestScriptsDoNotSleep(t *testing.T) {
	t.Parallel()
	sleepCommand := regexp.MustCompile(`\bexec\s+sleep(?:\s|$)`)
	files, err := filepath.Glob("tests/*.txtar")
	if err != nil {
		t.Fatalf("Glob(tests/*.txtar): %v", err)
	}
	for _, path := range files {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%s): %v", path, err)
		}
		for lineNo, line := range strings.Split(string(body), "\n") {
			if sleepCommand.MatchString(strings.TrimSpace(line)) {
				t.Errorf("%s:%d: exec sleep is forbidden; use cc-wait", path, lineNo+1)
			}
		}
	}
}

func ccWait(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) < 1 || len(args) > 3 {
		ts.Fatalf("usage: cc-wait <path> [<regexp> [<n>]] (negation unsupported)")
	}
	path := filepath.Join(ts.Getenv("WORK"), args[0])
	var pattern *regexp.Regexp
	if len(args) >= 2 {
		var err error
		pattern, err = regexp.Compile(args[1])
		ts.Check(err)
	}
	want := 1
	if len(args) == 3 {
		var err error
		want, err = strconv.Atoi(args[2])
		if err != nil || want < 1 {
			ts.Fatalf("cc-wait: count must be a positive integer: %q", args[2])
		}
	}
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(5 * time.Millisecond)
	defer poll.Stop()
	for {
		body, err := os.ReadFile(path)
		seen := 0
		if err == nil {
			if pattern == nil {
				return
			}
			for _, line := range strings.Split(string(body), "\n") {
				if pattern.MatchString(line) {
					seen++
				}
			}
			if seen >= want {
				return
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			ts.Fatalf("cc-wait %s: %v", args[0], err)
		}
		select {
		case <-poll.C:
		case <-deadline.C:
			patternText := ""
			if len(args) >= 2 {
				patternText = args[1]
			}
			ts.Fatalf("cc-wait %s pattern %q: saw %d matching lines, want %d within 30s", args[0], patternText, seen, want)
		}
	}
}

func ccInitRepo(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) > 1 {
		ts.Fatalf("usage: cc-init-repo [name]")
	}
	name := "repo"
	if len(args) == 1 {
		name = args[0]
	}
	work := ts.Getenv("WORK")
	repo := filepath.Join(work, name)
	remoteName := "remote.git"
	if name != "repo" {
		remoteName = name + "-remote.git"
	}
	remote := filepath.Join(work, remoteName)

	ts.Check(ts.Exec("git", "init", "-q", "-b", "main", repo))
	ts.Check(os.WriteFile(filepath.Join(repo, "README.md"), []byte("e2e\n"), 0o600))
	ts.Check(ts.Exec("git", "-C", repo, "add", "README.md"))
	ts.Check(ts.Exec("git", "-C", repo, "commit", "-q", "-m", "initial commit"))
	ts.Check(ts.Exec("git", "init", "-q", "--bare", remote))
	ts.Check(ts.Exec("git", "-C", repo, "remote", "add", "origin", remote))
	ts.Check(ts.Exec("git", "-C", repo, "push", "-q", "-u", "origin", "main"))
	publishRepoSettings(ts, name)
}

var perRepoKeys = []string{"tracker", "stacking", "deny", "checks", "compat_check", "mergify_sha", "verify_command"}

func pendingSettingsPath(ts *testscript.TestScript, name string) string {
	return filepath.Join(ts.Getenv("WORK"), "cc", "repo-settings", name+".toml")
}

func publishRepoSettings(ts *testscript.TestScript, name string) {
	body, err := os.ReadFile(pendingSettingsPath(ts, name))
	if err != nil {
		return
	}
	repo := filepath.Join(ts.Getenv("WORK"), name)
	if _, err := os.Stat(filepath.Join(repo, ".git")); err != nil {
		return
	}
	dest := filepath.Join(repo, config.SettingsFile)
	if current, err := os.ReadFile(dest); err == nil && string(current) == string(body) {
		return
	}
	ts.Check(os.WriteFile(dest, body, 0o600))
	ts.Check(ts.Exec("git", "-C", repo, "add", config.SettingsFile))
	ts.Check(ts.Exec("git", "-C", repo, "commit", "-q", "-m", "repo settings"))
	ts.Check(ts.Exec("git", "-C", repo, "push", "-q", "origin", "main"))
}

func ccConfig(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) != 1 {
		ts.Fatalf("usage: cc-config <name>")
	}
	body, err := os.ReadFile(filepath.Join(testdataDir, "config", args[0]+".toml"))
	ts.Check(err)
	body = []byte(strings.ReplaceAll(string(body), "{{agents}}", agentsDir))

	var doc map[string]any
	ts.Check(toml.Unmarshal(body, &doc))
	doc["port"] = freePort(ts)

	settingsDir := filepath.Join(ts.Getenv("WORK"), "cc", "repo-settings")
	ts.Check(os.MkdirAll(settingsDir, 0o700))
	repos, _ := doc["repo"].([]map[string]any)
	delete(doc, "repo")
	trackRepos(ts, repos)
	var names []string
	for _, block := range repos {
		name, _ := block["name"].(string)
		settings := map[string]any{}
		for _, key := range perRepoKeys {
			if v, ok := block[key]; ok {
				settings[key] = v
				delete(block, key)
			}
		}
		if len(settings) == 0 {
			continue
		}
		var buf bytes.Buffer
		ts.Check(toml.NewEncoder(&buf).Encode(settings))
		ts.Check(os.WriteFile(pendingSettingsPath(ts, name), buf.Bytes(), 0o600))
		names = append(names, name)
	}

	var out bytes.Buffer
	ts.Check(toml.NewEncoder(&out).Encode(doc))
	ts.Check(os.WriteFile(filepath.Join(ts.Getenv("WORK"), "cc", "config.toml"), out.Bytes(), 0o600))
	for _, name := range names {
		publishRepoSettings(ts, name)
	}
}

func trackRepos(ts *testscript.TestScript, blocks []map[string]any) {
	st, err := store.OpenStore(ts.Getenv("CC_DATABASE_URL"))
	ts.Check(err)
	defer func() { ts.Check(st.Close()) }()
	for _, block := range blocks {
		name, _ := block["name"].(string)
		remote, _ := block["remote"].(string)
		fullName, err := git.FullName(remote)
		ts.Check(err)
		checkout := config.CheckoutPath(ts.Getenv("CC_DATA_DIR"), fullName)
		if _, err := os.Lstat(checkout); errors.Is(err, os.ErrNotExist) {
			ts.Check(os.MkdirAll(filepath.Dir(checkout), 0o700))
			ts.Check(os.Symlink(filepath.Join(ts.Getenv("WORK"), name), checkout))
		}
		ts.Check(st.UpsertRepo(context.Background(), store.Repo{
			Name: fullName, Remote: remote, Phase: plan.RepoCloning, TrackedAt: time.Now(),
		}))
	}
}

func ccFakeGh(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) != 1 {
		ts.Fatalf("usage: cc-fake-gh <name>")
	}
	body, err := os.ReadFile(filepath.Join(testdataDir, "gh", args[0]+".json"))
	ts.Check(err)
	ts.Check(os.WriteFile(ts.Getenv("CC_GH_FIXTURE"), body, 0o600))
}

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

const daemonReady = "addr=http://"

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
	daemon.Env = append(scriptEnv(work, ts.Getenv("CC_DATABASE_URL")),
		"PATH="+ts.Getenv("PATH"), "TMPDIR="+ts.Getenv("TMPDIR"))
	daemon.Stdout = logFile
	daemon.Stderr = logFile
	ts.Check(daemon.Start())
	ts.Defer(func() {
		_ = daemon.Process.Kill()
		_ = daemon.Wait()
		_ = logFile.Close()
	})

	poll := time.NewTicker(5 * time.Millisecond)
	defer poll.Stop()
	deadline := time.After(30 * time.Second)
	for {
		body, err := os.ReadFile(logPath)
		if err == nil && strings.Contains(string(body), daemonReady) {
			return
		}
		select {
		case <-poll.C:
		case <-deadline:
			ts.Fatalf("cc did not log %q within the deadline:\n%s", daemonReady, body)
		}
	}
}

func setup(env *testscript.Env) error {
	dsn, drop, err := cctest.Create(context.Background())
	if err != nil {
		return err
	}
	env.Defer(func() {
		if err := drop(); err != nil {
			env.T().Log("drop test database:", err)
		}
	})
	env.Vars = append(env.Vars, scriptEnv(env.WorkDir, dsn)...)
	return nil
}

func scriptEnv(work, databaseURL string) []string {
	return []string{
		"HOME=" + work,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=Command Centre",
		"GIT_AUTHOR_EMAIL=cc@example.com",
		"GIT_COMMITTER_NAME=Command Centre",
		"GIT_COMMITTER_EMAIL=cc@example.com",
		"CC_DATA_DIR=" + filepath.Join(work, "cc-data"),
		"CC_DATABASE_URL=" + databaseURL,
		"CC_GH_FIXTURE=" + filepath.Join(work, "gh-fixture.json"),
		"CC_GH_LOG=" + filepath.Join(work, "gh.log"),
		"CC_TP_LOG=" + filepath.Join(work, "tp.log"),
		"CC_AGENT_LOG=" + filepath.Join(work, "agent.log"),
	}
}

func freePort(ts *testscript.TestScript) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	ts.Check(err)
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port
}
