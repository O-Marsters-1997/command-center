package gittest

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/adapter/command"
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
	out, err := command.Output(tb.Context(), dir, "git", args...)
	if err != nil {
		tb.Fatalf("run git: %v", err)
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
	if _, err := command.Output(tb.Context(), dir, "git", "rev-parse", "--verify", "--quiet", ref); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			return false
		}
		tb.Fatalf("resolve ref %q: %v", ref, err)
	}
	return true
}

func RunMain(m *testing.M) int {
	home, err := os.MkdirTemp("", "cc-home-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create home: %v\n", err)
		return 1
	}
	defer func() {
		if err := os.RemoveAll(home); err != nil {
			fmt.Fprintf(os.Stderr, "remove home: %v\n", err)
		}
	}()
	if err := os.Setenv("HOME", home); err != nil {
		fmt.Fprintf(os.Stderr, "set HOME: %v\n", err)
		return 1
	}
	return m.Run()
}

func identity(tb testing.TB, dir string) {
	tb.Helper()
	for key, value := range map[string]string{
		"user.name": "t", "user.email": "t@example.com", "commit.gpgsign": "false",
	} {
		Run(tb, dir, "config", "--local", key, value)
	}
}
