package plan_test

import (
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
