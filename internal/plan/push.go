package plan

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Policy is one repo's push-refusal policy: the default deny set every repo enforces plus this
// repo's own [[repo]] additions.
type Policy struct {
	Deny []string
}

var defaultDeny = []string{
	".github/**",
	SettingsFile,
	".mergify.yml",
	"CODEOWNERS",
	"**/package.json",
	"**/package-lock.json",
	"**/pnpm-lock.yaml",
	"**/yarn.lock",
	"**/bun.lock",
	"**/bun.lockb",
	"pnpm-workspace.yaml",
	".npmrc",
	".env*",
}

// PushRefused reports whether any changed path hits the policy's deny set, naming the first
// match. One hit refuses the whole diff: nothing partial is pushed.
func PushRefused(changedPaths []string, policy Policy) (bool, string) {
	patterns := make([]string, 0, len(defaultDeny)+len(policy.Deny))
	patterns = append(patterns, defaultDeny...)
	patterns = append(patterns, policy.Deny...)

	for _, path := range changedPaths {
		for _, pattern := range patterns {
			if denyMatch(pattern, path) {
				return true, path
			}
		}
	}
	return false, ""
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

type PushCandidate struct {
	URL           string
	LocalTip      string
	LastPushedTip string
}

// PushPlan selects every ticket whose local tip has moved past its last recorded push. A branch
// already at its last pushed tip is not re-attempted.
func PushPlan(candidates []PushCandidate) []string {
	var selected []string
	for _, c := range candidates {
		if c.LocalTip != "" && c.LocalTip != c.LastPushedTip {
			selected = append(selected, c.URL)
		}
	}
	return selected
}

// PRBody composes the body gh pr create --fill needs only for a stacked base: the "Merge after
// #N" line, since `gh pr create --base` makes an ordinary PR, not a GitHub Stack.
func PRBody(baseBranch string, blockerPRNumber int) string {
	if baseBranch == DefaultBaseBranch || blockerPRNumber == 0 {
		return ""
	}
	return fmt.Sprintf("Merge after #%d", blockerPRNumber)
}
