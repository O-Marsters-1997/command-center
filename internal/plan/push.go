package plan

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

type Tier string

const (
	TierUnknown   Tier = ""
	TierDenied    Tier = "denied"
	TierProtected Tier = "protected"
)

type PathMatch struct {
	Path    string `json:"path"`
	Rule    string `json:"rule"`
	Tier    Tier   `json:"tier"`
	Added   int    `json:"added"`
	Deleted int    `json:"deleted"`
}

type Policy struct {
	Deny    []string
	Protect []string
}

var defaultDeny = []string{SettingsFile, ".mergify.yml", ".env*", ".npmrc"}

var defaultProtect = []string{
	".github/**", "CODEOWNERS", "**/package.json", "**/package-lock.json", "**/pnpm-lock.yaml",
	"**/yarn.lock", "**/bun.lock", "**/bun.lockb", "pnpm-workspace.yaml",
}

func Classify(changed []string, p Policy) []PathMatch {
	deny := slices.Clone(p.Deny)
	for _, pattern := range defaultDeny {
		if pattern == SettingsFile || !slices.Contains(p.Protect, pattern) {
			deny = append(deny, pattern)
		}
	}
	protect := append(slices.Clone(defaultProtect), p.Protect...)

	var matches []PathMatch
	for _, path := range changed {
		if rule, ok := firstMatch(deny, path); ok {
			matches = append(matches, PathMatch{Path: path, Rule: rule, Tier: TierDenied})
		} else if rule, ok := firstMatch(protect, path); ok {
			matches = append(matches, PathMatch{Path: path, Rule: rule, Tier: TierProtected})
		}
	}
	return matches
}

const ProtectedLabel = "cc-protected"

func ProtectedSection(matches []PathMatch) string {
	if len(matches) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Protected paths\n")
	for _, m := range matches {
		fmt.Fprintf(&b, "\n- `%s` (rule `%s`) +%d −%d", m.Path, m.Rule, m.Added, m.Deleted)
	}
	return b.String()
}

func Denied(matches []PathMatch) []PathMatch { return inTier(matches, TierDenied) }

func Protected(matches []PathMatch) []PathMatch { return inTier(matches, TierProtected) }

func inTier(matches []PathMatch, tier Tier) []PathMatch {
	return slices.DeleteFunc(slices.Clone(matches), func(m PathMatch) bool { return m.Tier != tier })
}

func firstMatch(patterns []string, path string) (string, bool) {
	for _, pattern := range patterns {
		if denyMatch(pattern, path) {
			return pattern, true
		}
	}
	return "", false
}

func denyMatch(pattern, path string) bool {
	switch {
	case strings.HasSuffix(pattern, "/**"):
		dir := strings.TrimSuffix(pattern, "/**")
		return path == dir || strings.HasPrefix(path, dir+"/")
	case strings.HasPrefix(pattern, "**/"):
		return filepath.Base(path) == strings.TrimPrefix(pattern, "**/")
	case strings.HasSuffix(pattern, "*"):
		return strings.HasPrefix(filepath.Base(path), strings.TrimSuffix(pattern, "*"))
	default:
		return path == pattern
	}
}

// PRBody composes the "Merge after #N" line for a stacked base.
// GitHub's `gh pr create --base` creates an ordinary PR, not a stack.
func PRBody(baseBranch string, blockerPRNumber int) string {
	if baseBranch == DefaultBaseBranch || blockerPRNumber == 0 {
		return ""
	}
	return fmt.Sprintf("Merge after #%d", blockerPRNumber)
}
