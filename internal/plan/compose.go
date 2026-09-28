package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// Compose renders the prompt a launch authorises: the implement instruction for the ticket,
// plus a worked-example section when t.WorkedExampleBranch is set, so a later wave can read a
// blocker's diff instead of re-exploring. The implement instruction always leads, since a slash
// command has to open the prompt.
func Compose(t Ticket) string {
	prompt := "/implement " + t.URL
	if t.WorkedExampleBranch != "" {
		prompt += fmt.Sprintf(
			"\n\n## Worked example: %[1]s\n\nRead the diff:\n\n    git diff %[2]s...%[1]s",
			t.WorkedExampleBranch, defaultBranch)
	}
	return prompt
}

// ComposeExplore renders the prompt a launch's own explore run authorises: read the repository
// once for the whole launch and write a shared brief to path, naming every member ticket.
func ComposeExplore(tickets []Ticket, path string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Explore this repository once for the whole launch below, and write a brief "+
		"to %s covering:\n\n", path)
	b.WriteString("## File map\n\nWhere the packages, tests and templates that matter here live.\n\n")
	b.WriteString("## Conventions\n\nHow this codebase is written: idioms, package layout, error handling.\n\n")
	b.WriteString("## Seams\n\nWhere two or more of the tickets below are about to touch the same code.\n\n")
	b.WriteString("## Test commands\n\nHow to run the tests and lints this repo expects to pass.\n\n")
	b.WriteString("Keep the whole brief to about 3,000 tokens. Then one section per ticket:\n")
	for _, t := range tickets {
		fmt.Fprintf(&b, "\n## %s\n\nBranch: %s.", t.URL, t.Branch)
		if len(t.BlockedBy) > 0 {
			fmt.Fprintf(&b, " Blocked by: %s.", strings.Join(t.BlockedBy, ", "))
		}
	}
	return b.String()
}

const resolveSkillPath = "cc/skills/resolve-merge-conflict/SKILL.md"

// ComposeResolve renders the prompt a resolve run authorises: merge origin/main into the branch
// and follow the conflict-resolution skill, stopping short of its own final commit step.
func ComposeResolve(t Ticket) string {
	return fmt.Sprintf(
		"Merge origin/main into %s and follow %s to resolve every conflict. "+
			"Stage the resolution but stop before its own step 5: do not commit, and do not push.",
		t.Branch, resolveSkillPath)
}

const followUpSkillPath = "cc/skills/follow-up/SKILL.md"

// ComposeFollowUp renders the prompt a follow-up run authorises: the follow-up skill invocation,
// the operator's own typed instruction, and ciSection when the ticket is ci_failed -- composed by
// the caller, since this package never execs and so cannot fetch a log itself.
func ComposeFollowUp(text, ciSection string) string {
	prompt := fmt.Sprintf("Follow %s. Your instruction:\n\n%s", followUpSkillPath, text)
	if ciSection != "" {
		prompt += "\n\n" + ciSection
	}
	return prompt
}

// Hash fingerprints a composed prompt. Consent is bound to content (docs/command-centre-
// v1.md § 4b): a launch stores this at authorisation and the tick recomputes it at spawn
// time, refusing on mismatch.
func Hash(composed string) string {
	sum := sha256.Sum256([]byte(composed))
	return hex.EncodeToString(sum[:])
}
