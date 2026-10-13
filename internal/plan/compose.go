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

const followUpSkillPath = "cc/skills/follow-up/SKILL.md"

func ComposeFollowUp(text string) string {
	return fmt.Sprintf("Follow %s. Your instruction:\n\n%s", followUpSkillPath, text)
}

func ComposeAsking(questionPath string) string {
	return fmt.Sprintf(
		"\n\n## Asking the owner\n\n"+
			"If you need a decision only the owner can make, write one question to %s and end your turn. "+
			"The owner's answer resumes this session. Do not ask what you can decide yourself.",
		questionPath)
}

func ComposeMessage(text string, after Outcome) string {
	switch after {
	case OutcomeAsked:
		return fmt.Sprintf("The owner's answer to your question:\n\n%s", text)
	case OutcomeContinued:
		return fmt.Sprintf(
			"The owner interrupted you with a message. Your last tool call may have been cut off, "+
				"so check its effect before going on:\n\n%s", text)
	default:
		return fmt.Sprintf("The owner's follow-up instruction:\n\n%s", text)
	}
}

// Hash fingerprints a composed prompt: consent is bound to content, so a launch stores it at
// authorisation and the tick refuses to spawn on a mismatch.
func Hash(composed string) string {
	sum := sha256.Sum256([]byte(composed))
	return hex.EncodeToString(sum[:])
}
