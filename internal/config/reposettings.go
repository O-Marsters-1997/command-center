package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/O-Marsters-1997/command-center/internal/adapter/tracker"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/verdict"
)

// SettingsFile is the repo-root file holding one repo's settings, read from origin/main.
const SettingsFile = plan.SettingsFile

// SettingsParseError identifies the line of invalid repository settings.
type SettingsParseError struct {
	Line int
	Err  error
}

func (e *SettingsParseError) Error() string { return fmt.Sprintf("line %d: %v", e.Line, e.Err) }
func (e *SettingsParseError) Unwrap() error { return e.Err }

type Source string

const (
	SourceDefaults Source = "defaults"
	SourceFile     Source = "origin/main"
)

type RepoSettings = plan.RepoSettings

func DefaultRepoSettings() RepoSettings {
	return RepoSettings{Tracker: string(tracker.GitHub)}
}

// SettingsRef is the ref callers read SettingsFile from, never a branch or worktree.
const SettingsRef = "origin/main"

// ReadRepoSettings decodes the body of SettingsFile read from SettingsRef; found is false when
// the file is absent, which answers defaults. A malformed body, an unknown key or an invalid
// predicate is an error naming the line.
func ReadRepoSettings(body []byte, found bool) (RepoSettings, Source, error) {
	if !found {
		return DefaultRepoSettings(), SourceDefaults, nil
	}
	settings, err := ParseRepoSettings(string(body))
	if err != nil {
		return RepoSettings{}, "", err
	}
	return settings, SourceFile, nil
}

func ParseRepoSettings(body string) (RepoSettings, error) {
	settings := DefaultRepoSettings()
	md, err := toml.Decode(body, &settings)
	if err != nil {
		var perr toml.ParseError
		if errors.As(err, &perr) {
			return RepoSettings{}, &SettingsParseError{Line: perr.Position.Line, Err: err}
		}
		return RepoSettings{}, fmt.Errorf("decode repo settings: %w", err)
	}
	if settings.Tracker == "" {
		settings.Tracker = string(tracker.GitHub)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		key := undecoded[0]
		return RepoSettings{}, &SettingsParseError{
			Line: lineOfKey(body, key), Err: fmt.Errorf("unknown key %q", key.String()),
		}
	}
	if err := validatePredicate(settings.Checks, true); err != nil {
		return RepoSettings{}, &SettingsParseError{
			Line: lineOfKey(body, toml.Key{"checks"}), Err: fmt.Errorf("invalid checks: %w", err),
		}
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
	name := key[len(key)-1]
	for i, line := range strings.Split(body, "\n") {
		if namesKey(strings.TrimSpace(line), name) {
			return i + 1
		}
	}
	return 0
}

func namesKey(line, name string) bool {
	for offset := 0; ; {
		at := strings.Index(line[offset:], name)
		if at < 0 {
			return false
		}
		start := offset + at
		end := start + len(name)
		offset = start + 1
		if start > 0 && !strings.ContainsRune("[. \t", rune(line[start-1])) {
			continue
		}
		rest := strings.TrimLeft(line[end:], " \t")
		if strings.HasPrefix(rest, "=") || strings.HasPrefix(rest, "]") || strings.HasPrefix(line[end:], ".") {
			return true
		}
	}
}
