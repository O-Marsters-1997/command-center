package git_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/git"
)

var githubFullName = regexp.MustCompile(`^[A-Za-z0-9-]+/[A-Za-z0-9._-]+$`)

func FuzzFullName(f *testing.F) {
	f.Fuzz(func(t *testing.T, remote string) {
		normal := git.NormaliseRemote(remote)
		if !strings.HasPrefix(normal, "github.com/") ||
			!githubFullName.MatchString(strings.TrimPrefix(normal, "github.com/")) {
			_, _ = git.FullName(remote)
			return
		}
		fullName, err := git.FullName(remote)
		if err != nil {
			return
		}
		if !git.SameRemote(remote, "https://github.com/"+fullName) {
			t.Errorf("FullName(%q) = %q, but it does not name the same GitHub remote", remote, fullName)
		}
	})
}
