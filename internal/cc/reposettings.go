package cc

import (
	"context"
	"fmt"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/O-Marsters-1997/command-center/internal/tracker"
	"github.com/O-Marsters-1997/command-center/internal/verdict"
)

// repoSettingsFile is the per-repo settings file ReadRepoSettings reads from a tracked repo's
// own origin/main (plans/tracked-repos.md), never from a ticket's branch or worktree -- the
// file that tightens
// what an agent working in that repo may touch, so the file itself must be unreachable from the
// branch it would otherwise be tempted to loosen.
const repoSettingsFile = ".command-centre.toml"

// RepoSettings holds every per-repo key, decoded from repoSettingsFile with the same TOML tags
// [[repo]] used before this file existed, so moving a block across is a cut and paste. Its
// zero value is
// "defaults" for every field except Tracker, which ReadRepoSettings defaults to "github" the way
// LoadConfig once did.
type RepoSettings struct {
	// Tracker names which issue tracker this repo's tickets live in.
	Tracker     string            `toml:"tracker"`
	Stacking    bool              `toml:"stacking"`
	CompatCheck string            `toml:"compat_check"`
	MergifySHA  string            `toml:"mergify_sha"`
	Deny        []string          `toml:"deny"`
	Checks      verdict.Predicate `toml:"checks"`
	// VerifyCommand is the argv a clean refresh or restack is verified with before the row is
	// offered as sound (issue #110); empty means the repo opted out.
	VerifyCommand []string `toml:"verify_command"`
	// Generated names the paths this repo's build regenerates, glob-matched against a conflict
	// with origin/main; empty means the repo opted out.
	Generated []string `toml:"generated"`
	// BuildCommand is the argv that regenerates Generated's paths, run in the ticket's own
	// worktree before they are staged and committed; empty means the repo opted out.
	BuildCommand []string `toml:"build_command"`
}

// RepoSettingsSource says where a RepoSettings value came from.
type RepoSettingsSource string

const (
	SettingsFromFile     RepoSettingsSource = "file"
	SettingsFromDefaults RepoSettingsSource = "defaults"
)

// ReadRepoSettings reads checkout's per-repo settings from repoSettingsFile as committed on
// origin/main -- never checkout's working tree, a worktree beside it, or any branch other than
// the default one, so merging a change is the only way to change what binds an agent (the
// Scenario this covers). A missing file answers RepoSettings{Tracker: "github"} with source
// SettingsFromDefaults. A parse error, a key repoSettingsFile does not define, or a predicate
// that breaks verdict.Predicate's "exactly one" shape answers an error naming it.
func ReadRepoSettings(ctx context.Context, checkout string) (RepoSettings, RepoSettingsSource, error) {
	data, err := ShowFile(ctx, checkout, "origin/"+defaultBaseBranch, repoSettingsFile)
	if err != nil {
		if settingsFileMissing(err) {
			return RepoSettings{Tracker: string(tracker.GitHub)}, SettingsFromDefaults, nil
		}
		return RepoSettings{}, "", fmt.Errorf("read %s: %w", repoSettingsFile, err)
	}

	var settings RepoSettings
	meta, err := toml.Decode(data, &settings)
	if err != nil {
		return RepoSettings{}, "", fmt.Errorf("parse %s: %w", repoSettingsFile, err)
	}
	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		return RepoSettings{}, "", fmt.Errorf("%s: unknown key %q", repoSettingsFile, undecoded[0].String())
	}
	if err := settings.Checks.Validate(); err != nil {
		return RepoSettings{}, "", fmt.Errorf("%s: %w", repoSettingsFile, err)
	}
	if settings.Tracker == "" {
		settings.Tracker = string(tracker.GitHub)
	}
	return settings, SettingsFromFile, nil
}

// settingsFileMissing reports whether err is git show's own "no path at this ref" fatal, the one
// shape ReadRepoSettings treats as "nothing configured" rather than a refusal. git gives this no
// exit code of its own (128 also covers an unresolvable ref), so the message text -- stable
// across git versions -- is the only signal available. git phrases it two ways: "does not exist
// in '<ref>'" when the path has never been committed, and "exists on disk, but not in '<ref>'"
// when the working tree holds an uncommitted or not-yet-merged copy -- exactly this repo's own
// checkout before this PR's .command-centre.toml is merged.
func settingsFileMissing(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "does not exist in") || strings.Contains(msg, "but not in")
}
