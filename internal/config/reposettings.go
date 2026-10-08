package config

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/O-Marsters-1997/command-center/internal/git"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/tracker"
	"github.com/O-Marsters-1997/command-center/internal/verdict"
)

// SettingsFile is the repo-root file holding one repo's settings, read from origin/main.
const SettingsFile = plan.SettingsFile

const settingsRef = "origin/main"

// Source says where a repo's settings came from.
type Source string

const (
	SourceDefaults Source = "defaults"
	SourceFile     Source = "origin/main"
)

// RepoSettings is everything a target repo says about itself in SettingsFile. It is declared in
// internal/plan, which must stay pure and so cannot import this package.
type RepoSettings = plan.RepoSettings

// DefaultRepoSettings is what a repo without SettingsFile gets.
func DefaultRepoSettings() RepoSettings {
	return RepoSettings{Tracker: string(tracker.GitHub)}
}

// ReadRepoSettings reads SettingsFile from checkout's origin/main, never from a branch or
// worktree. A missing file answers defaults; a malformed file, an unknown key or an invalid
// predicate is an error naming the line.
func ReadRepoSettings(ctx context.Context, checkout string) (RepoSettings, Source, error) {
	present, err := git.HasFile(ctx, checkout, settingsRef, SettingsFile)
	if err != nil {
		return RepoSettings{}, "", fmt.Errorf("look for %s on %s: %w", SettingsFile, settingsRef, err)
	}
	if !present {
		return DefaultRepoSettings(), SourceDefaults, nil
	}
	body, err := git.ShowFile(ctx, checkout, settingsRef, SettingsFile)
	if err != nil {
		return RepoSettings{}, "", fmt.Errorf("read %s on %s: %w", SettingsFile, settingsRef, err)
	}
	settings, err := ParseRepoSettings(body)
	if err != nil {
		return RepoSettings{}, "", fmt.Errorf("%s on %s: %w", SettingsFile, settingsRef, err)
	}
	return settings, SourceFile, nil
}

// ParseRepoSettings decodes a SettingsFile body.
func ParseRepoSettings(body string) (RepoSettings, error) {
	settings := DefaultRepoSettings()
	md, err := toml.Decode(body, &settings)
	if err != nil {
		var perr toml.ParseError
		if errors.As(err, &perr) {
			return RepoSettings{}, fmt.Errorf("line %d: %s", perr.Position.Line, perr.Message)
		}
		return RepoSettings{}, err
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		key := undecoded[0]
		return RepoSettings{}, fmt.Errorf("line %d: unknown key %q", lineOfKey(body, key), key.String())
	}
	if err := validatePredicate(settings.Checks, true); err != nil {
		return RepoSettings{}, fmt.Errorf("line %d: invalid checks: %w", lineOfKey(body, toml.Key{"checks"}), err)
	}
	return settings, nil
}

func validatePredicate(p verdict.Predicate, root bool) error {
	if p.IsZero() {
		if root {
			return nil
		}
		return errors.New("empty predicate")
	}
	set := 0
	for _, used := range []bool{
		len(p.AllOf) > 0, len(p.AnyOf) > 0, p.Not != nil, p.Success != "", p.Skipped != "", p.AbsentOK != "", p.Author != "",
	} {
		if used {
			set++
		}
	}
	if set > 1 {
		return errors.New("a predicate node sets more than one of all_of, any_of, not, success, skipped, absent_ok, author")
	}
	children := append(append([]verdict.Predicate{}, p.AllOf...), p.AnyOf...)
	if p.Not != nil {
		children = append(children, *p.Not)
	}
	for _, child := range children {
		if err := validatePredicate(child, false); err != nil {
			return err
		}
	}
	return nil
}

func lineOfKey(body string, key toml.Key) int {
	pattern := regexp.MustCompile(`(^|[\s\[.])` + regexp.QuoteMeta(key[len(key)-1]) + `(\s*=|\s*\]|\.)`)
	for i, line := range strings.Split(body, "\n") {
		if pattern.MatchString(strings.TrimSpace(line)) {
			return i + 1
		}
	}
	return 0
}
