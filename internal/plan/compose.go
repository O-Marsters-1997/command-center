package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// Compose renders the prompt a launch authorises. The implement instruction always leads,
// since a slash command has to open the prompt.
func Compose(t Ticket) string {
	prompt := "/implement " + t.URL
	if t.WorkedExampleBranch != "" {
		prompt += fmt.Sprintf(
			"\n\n## Worked example: %[1]s\n\nRead the diff:\n\n    git diff %[2]s...%[1]s",
			t.WorkedExampleBranch, DefaultBaseBranch)
	}
	return prompt
}

const resolveSkillPath = "cc/skills/resolve-merge-conflict/SKILL.md"

func ComposeResolve(t Ticket) string {
	return fmt.Sprintf(
		"Merge origin/main into %s and follow %s to resolve every conflict. "+
			"Stage the resolution but stop before its own step 5: do not commit, and do not push.",
		t.Branch, resolveSkillPath)
}

const followUpSkillPath = "cc/skills/follow-up/SKILL.md"

func ComposeFollowUp(text string) string {
	return fmt.Sprintf("Follow %s. Your instruction:\n\n%s", followUpSkillPath, text)
}

// Hash fingerprints a composed prompt: consent is bound to content, so a launch stores it at
// authorisation and the tick refuses to spawn on a mismatch.
func Hash(composed string) string {
	sum := sha256.Sum256([]byte(composed))
	return hex.EncodeToString(sum[:])
}
