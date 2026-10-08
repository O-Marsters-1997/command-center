package gh

import (
	"slices"
	"testing"
)

func TestDecodePushableKeepsWritableUnarchivedReposAcrossPages(t *testing.T) {
	t.Parallel()

	raw := []byte(`[
	  {"full_name":"acme/api","ssh_url":"git@github.com:acme/api.git","default_branch":"main","permissions":{"push":true}},
	  {"full_name":"acme/ro","ssh_url":"git@github.com:acme/ro.git","default_branch":"main","permissions":{"push":false}}
	][
	  {"full_name":"acme/old","ssh_url":"git@github.com:acme/old.git","default_branch":"main","archived":true,"permissions":{"push":true}},
	  {"full_name":"acme/web","ssh_url":"git@github.com:acme/web.git","default_branch":"trunk","permissions":{"push":true}}
	]`)

	got, err := decodePushable(raw)
	if err != nil {
		t.Fatalf("decodePushable() error = %v", err)
	}
	want := []RepoSummary{
		{FullName: "acme/api", SSHURL: "git@github.com:acme/api.git", DefaultBranch: "main"},
		{FullName: "acme/web", SSHURL: "git@github.com:acme/web.git", DefaultBranch: "trunk"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("decodePushable() = %+v, want %+v", got, want)
	}
}
