package gh_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/gh"
)

func TestCLI_PushableRepos(t *testing.T) {
	t.Parallel()

	raw := []byte(`[
	  {"full_name":"acme/api","ssh_url":"git@github.com:acme/api.git","default_branch":"main","permissions":{"push":true}},
	  {"full_name":"acme/ro","ssh_url":"git@github.com:acme/ro.git","default_branch":"main","permissions":{"push":false}}
	][
	  {"full_name":"acme/old","ssh_url":"git@github.com:acme/old.git","default_branch":"main",
	   "archived":true,"permissions":{"push":true}},
	  {"full_name":"acme/web","ssh_url":"git@github.com:acme/web.git","default_branch":"trunk","permissions":{"push":true}}
	]`)
	cli := fakeGH(func([]string) ([]byte, error) { return raw, nil })

	got, err := cli.PushableRepos(t.Context())
	if err != nil {
		t.Fatalf("PushableRepos() error = %v", err)
	}
	want := []gh.RepoSummary{
		{FullName: "acme/api", SSHURL: "git@github.com:acme/api.git", DefaultBranch: "main"},
		{FullName: "acme/web", SSHURL: "git@github.com:acme/web.git", DefaultBranch: "trunk"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("PushableRepos() mismatch (-want +got):\n%s", diff)
	}
}

func TestCLI_ReadRepoSettings(t *testing.T) {
	t.Parallel()

	t.Run("decodes the merge settings", func(t *testing.T) {
		t.Parallel()
		out := []byte(`{"allow_merge_commit":false,"allow_rebase_merge":true,"default_branch":"main"}`)
		cli := fakeGH(func([]string) ([]byte, error) { return out, nil })

		got, err := cli.ReadRepoSettings(t.Context(), "acme/api")
		if err != nil {
			t.Fatalf("ReadRepoSettings() error = %v", err)
		}
		want := gh.RepoSettings{AllowRebaseMerge: true, DefaultBranch: "main"}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("ReadRepoSettings() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("malformed output", func(t *testing.T) {
		t.Parallel()
		raw := readFixture(t, "malformed_repo_settings.json")
		cli := fakeGH(func([]string) ([]byte, error) { return raw, nil })

		if _, err := cli.ReadRepoSettings(t.Context(), "acme/api"); err == nil {
			t.Error("ReadRepoSettings(malformed) error = nil, want one")
		}
	})
}

func TestCLI_RepoRemote(t *testing.T) {
	t.Parallel()

	t.Run("returns the ssh url", func(t *testing.T) {
		t.Parallel()
		cli := fakeGH(func([]string) ([]byte, error) { return []byte(`{"ssh_url":"git@github.com:acme/api.git"}`), nil })

		got, err := cli.RepoRemote(t.Context(), "acme/api")
		if err != nil {
			t.Fatalf("RepoRemote() error = %v", err)
		}
		if want := "git@github.com:acme/api.git"; got != want {
			t.Errorf("RepoRemote(acme/api) = %q, want %q", got, want)
		}
	})

	t.Run("no ssh url is an error", func(t *testing.T) {
		t.Parallel()
		cli := fakeGH(func([]string) ([]byte, error) { return []byte(`{}`), nil })

		if _, err := cli.RepoRemote(t.Context(), "acme/api"); err == nil {
			t.Error("RepoRemote(no ssh_url) error = nil, want one")
		}
	})
}
