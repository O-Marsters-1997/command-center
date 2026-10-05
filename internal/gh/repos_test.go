package gh

import "testing"

func TestDecodeRepoPagesConcatenatesEachPaginatedArray(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "pushable_repos_paginated.json")
	got, err := decodeRepoPages(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("decodeRepoPages = %d repos, want 3 across both pages", len(got))
	}
	want := []string{"acme/alpha", "acme/beta", "acme/gamma"}
	for i, name := range want {
		if got[i].FullName != name {
			t.Errorf("repo %d = %q, want %q", i, got[i].FullName, name)
		}
	}
}

func TestPushableFromDropsReadOnlyAndArchivedRepos(t *testing.T) {
	t.Parallel()

	pages := []rawRepo{
		{FullName: "acme/pushable", SSHURL: "git@github.com:acme/pushable.git", Permissions: rawRepoPermissions{Push: true}},
		{FullName: "acme/read-only", Permissions: rawRepoPermissions{Push: false}},
		{FullName: "acme/archived", Archived: true, Permissions: rawRepoPermissions{Push: true}},
	}

	got := pushableFrom(pages)
	if len(got) != 1 || got[0].FullName != "acme/pushable" {
		t.Fatalf("pushableFrom = %+v, want only acme/pushable", got)
	}
}
