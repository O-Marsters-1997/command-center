package plan

import (
	"fmt"
	"path/filepath"
	"strings"
)

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

// PRBody composes the "Merge after #N" line for a stacked base.
// GitHub's `gh pr create --base` creates an ordinary PR, not a stack.
func PRBody(baseBranch string, blockerPRNumber int) string {
	if baseBranch == DefaultBaseBranch || blockerPRNumber == 0 {
		return ""
	}
	return fmt.Sprintf("Merge after #%d", blockerPRNumber)
}
