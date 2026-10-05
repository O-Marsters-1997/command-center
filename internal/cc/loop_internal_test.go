package cc

import "testing"

// TestNewLoopDefaultsEachRepoTrackerToGitHub guards against a regression: applyImportIntents
// runs ahead of this tick's own refreshRepoSettings, and on the very first tick (or any tick
// before one has ever succeeded) l.repos is exactly what NewLoop built it as. A [[repo]] block
// can no longer set tracker at all, so if NewLoop left it at its TOML zero value, every import
// on that tick would fail with tracker.New's "no source for kind \"\"" rather than resolving
// GitHub, the same default a freshly read (or missing) .command-centre.toml gets.
func TestNewLoopDefaultsEachRepoTrackerToGitHub(t *testing.T) {
	cfg := Config{Repos: []Repo{{Name: "alpha", Remote: "git@github.com:acme/alpha.git"}}}
	loop := NewLoop(nil, nil, nil, cfg, Workspace{}, nil)

	if got := loop.repos[0].Tracker; got != "github" {
		t.Errorf("repos[0].Tracker = %q, want the github default", got)
	}
}
