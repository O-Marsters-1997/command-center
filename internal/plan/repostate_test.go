package plan_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestNormaliseRemote(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		url  string
		want string
	}{
		{name: "https", url: "https://github.com/Owner/Repo.git", want: "github.com/owner/repo"},
		{name: "scp ssh", url: "git@github.com:Owner/Repo.git", want: "github.com/owner/repo"},
		{name: "ssh scheme with user", url: "ssh://git@github.com/Owner/Repo/", want: "github.com/owner/repo"},
		{name: "padded", url: "  https://github.com/o/r  ", want: "github.com/o/r"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := plan.NormaliseRemote(tt.url); got != tt.want {
				t.Errorf("NormaliseRemote(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestFullName(t *testing.T) {
	t.Parallel()

	for _, remote := range []string{
		"git@github.com:O-Marsters-1997/command-center.git",
		"https://github.com/O-Marsters-1997/command-center",
		"ssh://git@github.com/O-Marsters-1997/command-center.git/",
	} {
		t.Run(remote, func(t *testing.T) {
			t.Parallel()
			got, err := plan.FullName(remote)
			if err != nil || got != "O-Marsters-1997/command-center" {
				t.Errorf("FullName(%q) = %q, %v, want O-Marsters-1997/command-center, nil", remote, got, err)
			}
		})
	}

	t.Run("errors", func(t *testing.T) {
		t.Parallel()
		for _, remote := range []string{
			"", "/tmp/remote.git", "https://github.com/o", "https://host/a/b/c", "git@github.com:../evil.git",
			"https://github.com/o/..", "githuB.Com:0/.git.git", "git@github.com:owner/repo.git.git",
			"https://github.com/owner/repo.git.git",
		} {
			if got, err := plan.FullName(remote); err == nil {
				t.Errorf("FullName(%q) = %q, want an error", remote, got)
			}
		}
	})
}

var githubFullName = regexp.MustCompile(`^[A-Za-z0-9-]+/[A-Za-z0-9._-]+$`)

func FuzzFullName(f *testing.F) {
	f.Fuzz(func(t *testing.T, remote string) {
		fullName, err := plan.FullName(remote)
		if err != nil {
			return
		}
		normal := plan.NormaliseRemote(remote)
		if !strings.HasPrefix(normal, "github.com/") ||
			!githubFullName.MatchString(strings.TrimPrefix(normal, "github.com/")) {
			return
		}
		if normal != plan.NormaliseRemote("https://github.com/"+fullName) {
			t.Errorf("FullName(%q) = %q, which does not name the same GitHub remote", remote, fullName)
		}
	})
}

func TestRepoRefusal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		settings   plan.MergeSettings
		wantKind   plan.RefusalKind
		wantReason string
	}{
		{name: "squash only on main passes", settings: plan.MergeSettings{DefaultBranch: "main"}},
		{
			name:     "merge commits refuse",
			settings: plan.MergeSettings{AllowMergeCommit: true, DefaultBranch: "main"},
			wantKind: plan.RefusalMergeSettings, wantReason: "allow_merge_commit",
		},
		{
			name:     "rebase merges refuse",
			settings: plan.MergeSettings{AllowRebaseMerge: true, DefaultBranch: "main"},
			wantKind: plan.RefusalMergeSettings, wantReason: "allow_rebase_merge",
		},
		{
			name:     "a master default refuses with the rename hint",
			settings: plan.MergeSettings{DefaultBranch: "master"},
			wantKind: plan.RefusalDefaultBranch, wantReason: "rename it on GitHub",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			kind, reason := plan.RepoRefusal(tt.settings)
			if kind != tt.wantKind || !strings.Contains(reason, tt.wantReason) {
				t.Errorf("RepoRefusal(%+v) = %q, %q, want %q and a reason naming %q",
					tt.settings, kind, reason, tt.wantKind, tt.wantReason)
			}
		})
	}
}
