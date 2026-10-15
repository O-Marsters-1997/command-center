package gittest_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/gittest"
)

func TestInitConfiguresIdentityLocally(t *testing.T) {
	t.Parallel()
	repo := gittest.Init(t)
	for key, want := range map[string]string{
		"user.name": "t", "user.email": "t@example.com", "commit.gpgsign": "false",
	} {
		if got := gittest.Run(t, repo, "config", "--local", "--get", key); got != want {
			t.Errorf("config %s = %q, want %q", key, got, want)
		}
	}
	if !gittest.HasRef(t, repo, "refs/heads/main") {
		t.Error("Init did not create main")
	}
	if got := gittest.Run(t, repo, "log", "-1", "--format=%an <%ae>"); got != "t <t@example.com>" {
		t.Errorf("initial commit identity = %q, want t <t@example.com>", got)
	}
}

func TestWithOriginTracksMain(t *testing.T) {
	t.Parallel()
	repo, origin := gittest.WithOrigin(t)
	if got := gittest.Run(t, repo, "remote", "get-url", "origin"); got != origin {
		t.Errorf("origin URL = %q, want %q", got, origin)
	}
	if !gittest.HasRef(t, repo, "refs/remotes/origin/main") {
		t.Error("clone does not track origin/main")
	}
	if got := gittest.Run(t, origin, "config", "--local", "--get", "user.email"); got != "t@example.com" {
		t.Errorf("bare origin identity = %q, want t@example.com", got)
	}
}

func TestCommitAndHasRef(t *testing.T) {
	t.Parallel()
	repo := gittest.Init(t)
	sha := gittest.Commit(t, repo, "notes.txt", "notes\n")
	if got := gittest.Run(t, repo, "rev-parse", "HEAD"); got != sha {
		t.Errorf("HEAD = %q, want %q", got, sha)
	}
	if got, err := os.ReadFile(filepath.Join(repo, "notes.txt")); err != nil || string(got) != "notes\n" {
		t.Errorf("committed file = %q, %v, want notes", got, err)
	}
	if gittest.HasRef(t, repo, "refs/heads/absent") {
		t.Error("missing ref resolves")
	}
}

func TestHasRefFailsOnOtherGitErrors(t *testing.T) {
	if os.Getenv("GITTEST_HASREF_CHILD") == "1" {
		gittest.HasRef(t, t.TempDir(), "HEAD")
		return
	}
	t.Parallel()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHasRefFailsOnOtherGitErrors$")
	cmd.Env = append(os.Environ(), "GITTEST_HASREF_CHILD=1")
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "resolve ref") {
		t.Errorf("HasRef(non-repo) = %v, %q, want a ref-resolution test failure", err, out)
	}
}
