package cc_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
)

// commitsScript is the absolute path to testdata/agents/commits.sh (the fake agent that
// commits a file and exits 0), which lives at the module root rather than under internal/cc.
func commitsScript(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs("../../testdata/agents/commits.sh")
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// gitRepo creates a real git repo at a temp dir so commits.sh has something to commit into.
// The git identity is set via t.Setenv, not just on the setup commands' own exec.Cmd, because
// Spawn's env is os.Environ() minus the API key — it must inherit the same identity commits.sh
// needs to `git commit` inside the spawned process.
func gitRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("GIT_AUTHOR_NAME", "Command Centre")
	t.Setenv("GIT_AUTHOR_EMAIL", "cc@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Command Centre")
	t.Setenv("GIT_COMMITTER_EMAIL", "cc@example.com")

	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = os.Environ()
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("commit", "-q", "--allow-empty", "-m", "initial")
	return dir
}

func TestProcessRunnerSpawnRunsTheAgentWithSubstitutedArgvAndRedirectedOutput(t *testing.T) {
	worktree := gitRepo(t)
	settingsPath := filepath.Join(t.TempDir(), "agent.json")
	if err := cc.WriteAgentSettings(settingsPath); err != nil {
		t.Fatal(err)
	}
	promptPath := filepath.Join(t.TempDir(), "prompt.txt")
	if err := os.WriteFile(promptPath, []byte("/implement sandbox://CC-1"), 0o600); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(t.TempDir(), "run.jsonl")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logFile.Close() })

	t.Setenv("ANTHROPIC_API_KEY", "test-secret-key")

	cfg := cc.SpawnConfig{
		AgentCommand: []string{commitsScript(t), "{worktree}", "{settings}", "{prompt_file}"},
		WorktreePath: worktree,
		SettingsPath: settingsPath,
		PromptPath:   promptPath,
		LogFile:      logFile,
	}
	result, err := cc.ProcessRunner{}.Spawn(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if result.Pid == 0 {
		t.Fatal("Spawn returned a zero pid")
	}

	out := waitForCommit(t, worktree, "commit from commits.sh")
	if !strings.Contains(out, "commit from commits.sh") {
		t.Errorf("git log = %q, want the agent's commit", out)
	}

	logged, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(logged) != 0 {
		t.Errorf("commits.sh wrote no stdout/stderr, but the log file has content: %q", logged)
	}
}

// waitForFile polls for path to exist, failing the test after a short deadline. Spawn does not
// wait on the process, so the test must poll for the side effect it produces instead.
func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s did not appear within the deadline", path)
}

// waitForCommit polls `git log` until it contains want, failing after a short deadline. Writing
// agent.txt and committing it are two separate steps in commits.sh, so polling for the file
// alone races the commit that follows it.
func waitForCommit(t *testing.T, worktree, want string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var out []byte
	for time.Now().Before(deadline) {
		out, _ = exec.Command("git", "-C", worktree, "log", "--oneline").CombinedOutput()
		if strings.Contains(string(out), want) {
			return string(out)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("git log in %s never contained %q; last seen:\n%s", worktree, want, out)
	return ""
}

func TestProcessRunnerSpawnStripsAnthropicAPIKey(t *testing.T) {
	worktree := t.TempDir()
	settingsPath := filepath.Join(t.TempDir(), "agent.json")
	promptPath := filepath.Join(t.TempDir(), "prompt.txt")
	if err := os.WriteFile(promptPath, []byte("prompt"), 0o600); err != nil {
		t.Fatal(err)
	}
	envDump := filepath.Join(worktree, "env.txt")
	script := "#!/bin/sh\nenv > " + envDump + ".tmp && mv " + envDump + ".tmp " + envDump + "\n"
	scriptPath := filepath.Join(t.TempDir(), "dump-env.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	logFile, err := os.Create(filepath.Join(t.TempDir(), "run.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logFile.Close() })

	t.Setenv("ANTHROPIC_API_KEY", "test-secret-key")

	cfg := cc.SpawnConfig{
		AgentCommand: []string{scriptPath},
		WorktreePath: worktree,
		SettingsPath: settingsPath,
		PromptPath:   promptPath,
		LogFile:      logFile,
	}
	if _, err := (cc.ProcessRunner{}).Spawn(t.Context(), cfg); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	waitForFile(t, envDump)
	env, err := os.ReadFile(envDump)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(env), "ANTHROPIC_API_KEY") {
		t.Errorf("spawned process environment still carries ANTHROPIC_API_KEY:\n%s", env)
	}
}

func TestProcessRunnerSpawnSetsANewProcessGroup(t *testing.T) {
	worktree := t.TempDir()
	settingsPath := filepath.Join(t.TempDir(), "agent.json")
	promptPath := filepath.Join(t.TempDir(), "prompt.txt")
	if err := os.WriteFile(promptPath, []byte("prompt"), 0o600); err != nil {
		t.Fatal(err)
	}
	pgidDump := filepath.Join(worktree, "pgid.txt")
	// Written via a temp file plus rename, not a direct redirect: redirection truncates the
	// target to empty before ps produces any output, and waitForFile would then race an
	// existing-but-still-empty file.
	script := "#!/bin/sh\nps -o pgid= -p $$ > " + pgidDump + ".tmp && mv " + pgidDump + ".tmp " + pgidDump + "\n"
	scriptPath := filepath.Join(t.TempDir(), "dump-pgid.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	logFile, err := os.Create(filepath.Join(t.TempDir(), "run.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logFile.Close() })

	cfg := cc.SpawnConfig{
		AgentCommand: []string{scriptPath},
		WorktreePath: worktree,
		SettingsPath: settingsPath,
		PromptPath:   promptPath,
		LogFile:      logFile,
	}
	result, err := (cc.ProcessRunner{}).Spawn(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	waitForFile(t, pgidDump)
	raw, err := os.ReadFile(pgidDump)
	if err != nil {
		t.Fatal(err)
	}
	gotPgid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("parse recorded pgid %q: %v", raw, err)
	}
	if gotPgid != result.Pid {
		t.Errorf("pgid = %d, want the leader's own pid %d (Setpgid should make it its own group leader)",
			gotPgid, result.Pid)
	}
}
