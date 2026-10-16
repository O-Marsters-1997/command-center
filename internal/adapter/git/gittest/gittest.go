package gittest

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func Init(tb testing.TB) string {
	tb.Helper()
	repo := filepath.Join(tb.TempDir(), "repo")
	Run(tb, ".", "init", "-q", "-b", "main", repo)
	identity(tb, repo)
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("hi\n"), 0o600); err != nil {
		tb.Fatalf("write initial file: %v", err)
	}
	Run(tb, repo, "add", "README.md")
	Run(tb, repo, "commit", "-q", "-m", "initial")
	return repo
}

func WithOrigin(tb testing.TB) (repo, origin string) {
	tb.Helper()
	origin = filepath.Join(tb.TempDir(), "remote.git")
	Run(tb, ".", "init", "-q", "-b", "main", "--bare", origin)
	identity(tb, origin)
	seed := Init(tb)
	Run(tb, seed, "remote", "add", "origin", origin)
	Run(tb, seed, "push", "-q", "origin", "main")
	repo = filepath.Join(tb.TempDir(), "repo")
	Run(tb, ".", "clone", "-q", origin, repo)
	identity(tb, repo)
	return repo, origin
}

func Run(tb testing.TB, dir string, args ...string) string {
	tb.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		tb.Fatalf("git %v in %s: %v: %s", args, dir, err, stderr.String())
	}
	return strings.TrimSpace(string(out))
}

func Commit(tb testing.TB, dir, file, body string) (sha string) {
	tb.Helper()
	path := filepath.Join(dir, file)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		tb.Fatalf("create commit directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		tb.Fatalf("write commit file: %v", err)
	}
	Run(tb, dir, "add", "--", file)
	Run(tb, dir, "commit", "-q", "-m", "update "+file)
	return Run(tb, dir, "rev-parse", "HEAD")
}

// HasRef reports whether ref resolves; a git failure other than exit 1 fails tb.
func HasRef(tb testing.TB, dir, ref string) bool {
	tb.Helper()
	cmd := exec.Command("git", "rev-parse", "--verify", "--quiet", ref)
	cmd.Dir = dir
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if _, err := cmd.Output(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			return false
		}
		tb.Fatalf("resolve ref %q in %s: %v: %s", ref, dir, err, stderr.String())
	}
	return true
}

func identity(tb testing.TB, dir string) {
	tb.Helper()
	for key, value := range map[string]string{
		"user.name": "t", "user.email": "t@example.com", "commit.gpgsign": "false",
	} {
		Run(tb, dir, "config", "--local", key, value)
	}
}
