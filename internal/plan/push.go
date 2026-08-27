package plan

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Policy is one repo's push-refusal policy: the default set every repo enforces (docs/prd-
// command-centre.md § The agent edits the CI config, boundary 2) plus this repo's own [[repo]]
// deny additions.
type Policy struct {
	Deny []string
}

// defaultDeny is the set every repo enforces before any per-repo addition: CI config, the file
// the app's own verdict is transcribed from, ownership, every package manifest and lockfile,
// and env files -- the paths a real deploy or a secret-bearing install step reads before any
// application code compiles.
var defaultDeny = []string{
	".github/**",
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
// match so the row can list it (docs/prds/prd-command-centre.md § The states, needs you). One hit
// refuses the whole diff: nothing is force-pushed and nothing partial is pushed.
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

// denyMatch matches one deny pattern against one changed path. Three shapes cover every default
// entry and every per-repo addition seen in practice: a directory prefix ("x/**"), a filename at
// any depth ("**/x"), a basename prefix ("x*"), and otherwise a literal path -- the last also
// serving the exact-match rule a per-repo addition like "scripts/gen.sh" needs.
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

// PushCandidate is one task's push-eligibility facts: the branch's current local tip, and the
// tip its last recorded push (if any) covered.
type PushCandidate struct {
	TicketURL     string
	LocalTip      string
	LastPushedTip string
}

// PushPlan selects every ticket whose local tip has moved past its last recorded push -- every
// app-owned branch the tick must attempt to push this cycle (docs/prds/prd-command-centre.md § Phase
// 4). A branch already at its last pushed tip is not re-attempted (inv. 20).
func PushPlan(candidates []PushCandidate) []string {
	var selected []string
	for _, c := range candidates {
		if c.LocalTip != "" && c.LocalTip != c.LastPushedTip {
			selected = append(selected, c.TicketURL)
		}
	}
	return selected
}

// PRBody composes the body override gh pr create --fill needs only for a stacked base: the
// "Merge after #N" line a reviewer needs since `gh pr create --base` makes an ordinary PR, not a
// GitHub Stack. Unreachable in Phase 1 (stacking = false, so baseBranch is always main), wired
// now so Phase 2 does not retrofit it.
func PRBody(baseBranch string, blockerPRNumber int) string {
	if baseBranch == defaultBranch || blockerPRNumber == 0 {
		return ""
	}
	return fmt.Sprintf("Merge after #%d", blockerPRNumber)
}
