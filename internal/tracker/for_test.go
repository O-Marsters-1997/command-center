package tracker

import "testing"

func TestFor(t *testing.T) {
	t.Parallel()

	src, err := For("https://github.com/O-Marsters-1997/command-center/issues/105")
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	gh, ok := src.(*githubSource)
	if !ok {
		t.Fatalf("For returned %T, want *githubSource", src)
	}
	if gh.owner != "O-Marsters-1997" || gh.repo != "command-center" {
		t.Errorf("owner/repo = %s/%s, want O-Marsters-1997/command-center", gh.owner, gh.repo)
	}
	if gh.run == nil {
		t.Error("For built a githubSource with no run func")
	}
}

func TestForRejectsUnknownHost(t *testing.T) {
	t.Parallel()

	_, err := For("https://gitlab.com/owner/repo/issues/1")
	if err == nil {
		t.Fatal("For accepted an unimplemented host")
	}
}

func TestForRejectsUnparseableURL(t *testing.T) {
	t.Parallel()

	tests := []string{
		"not a url at all \x7f",
		"https://github.com/",
		"https://github.com/owner-only",
	}
	for _, raw := range tests {
		if _, err := For(raw); err == nil {
			t.Errorf("For(%q) did not error", raw)
		}
	}
}
