package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// Compose renders the prompt a launch authorises: the implement instruction for the ticket.
func Compose(t Ticket) string {
	return "/implement " + t.URL
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

// ComposeFollowUp renders the prompt a follow-up run authorises: the follow-up skill invocation
// plus the operator's own typed instruction.
func ComposeFollowUp(text string) string {
	return fmt.Sprintf("Follow %s. Your instruction:\n\n%s", followUpSkillPath, text)
}

// Hash fingerprints a composed prompt. Consent is bound to content (docs/command-centre-
// v1.md § 4b): a launch stores this at authorisation and the tick recomputes it at spawn
// time, refusing on mismatch.
func Hash(composed string) string {
	sum := sha256.Sum256([]byte(composed))
	return hex.EncodeToString(sum[:])
}
