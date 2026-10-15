package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

func Compose(t Ticket) string {
	prompt := "/implement " + string(t.URL)
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
	case OutcomePush, OutcomeFailed, OutcomeCutFailed:
	}
	return fmt.Sprintf("The owner's follow-up instruction:\n\n%s", text)
}

func Hash(composed string) string {
	sum := sha256.Sum256([]byte(composed))
	return hex.EncodeToString(sum[:])
}
