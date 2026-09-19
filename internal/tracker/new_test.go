package tracker

import "testing"

func TestNew(t *testing.T) {
	t.Parallel()

	src, err := New(GitHub, "github.com/o-marsters-1997/command-center")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	gh, ok := src.(*githubSource)
	if !ok {
		t.Fatalf("New returned %T, want *githubSource", src)
	}
	if gh.owner != "o-marsters-1997" || gh.repo != "command-center" {
		t.Errorf("owner/repo = %s/%s, want o-marsters-1997/command-center", gh.owner, gh.repo)
	}
	if gh.run == nil {
		t.Error("New built a githubSource with no run func")
	}
}

func TestNewRejectsUnknownKind(t *testing.T) {
	t.Parallel()

	_, err := New("linear", "github.com/owner/repo")
	if err == nil {
		t.Fatal("New accepted an unimplemented kind")
	}
}

func TestNewRejectsUnparseableRemote(t *testing.T) {
	t.Parallel()

	tests := []string{"", "owner-only"}
	for _, remote := range tests {
		if _, err := New(GitHub, remote); err == nil {
			t.Errorf("New(GitHub, %q) did not error", remote)
		}
	}
}
