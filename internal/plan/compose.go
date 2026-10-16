package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/O-Marsters-1997/command-center/internal/plan/option"
)

type Effort string

const (
	EffortLow    Effort = "low"
	EffortMedium Effort = "medium"
	EffortHigh   Effort = "high"
)

func AllEfforts() []Effort { return []Effort{EffortLow, EffortMedium, EffortHigh} }

func ParseEffort(s string) (Effort, error) {
	for _, e := range AllEfforts() {
		if string(e) == s {
			return e, nil
		}
	}
	return "", fmt.Errorf("unknown effort %q", s)
}

type AgentChoice struct {
	Model  string
	Effort option.Option[Effort]
}

func Compose(t Ticket) string {
	prompt := "/implement " + string(t.URL)
	if t.WorkedExampleBranch != "" {
		prompt += fmt.Sprintf(
			"\n\n## Worked example: %[1]s\n\nRead the diff:\n\n    git diff %[2]s...%[1]s",
			t.WorkedExampleBranch, DefaultBaseBranch)
	}
	if t.LaunchContext != "" {
		prompt += "\n\n## Launch context\n\n" + t.LaunchContext
	}
	return prompt
}

func ComposeFollowUp(text string) string {
	return fmt.Sprintf(
		"You were not given a fresh worktree. This one already carries a branch, a diff and a git history "+
			"a prior run produced; nothing remembers what that run was thinking, only what it committed. "+
			"Run `git status`, `git diff` and `git log -5`, and read the PR description if one exists, "+
			"before you touch anything.\n\n"+
			"Below is the operator's own instruction, typed for this run. It is the whole of your task, "+
			"not a restatement of the original ticket, and the ticket's acceptance criteria are not "+
			"automatically still the bar.\n\n"+
			"Carry it out, run the checks the repo defines for the code you touched, and commit each "+
			"distinct change separately. Do not push: the daemon that spawned you reads this worktree's "+
			"commits to decide when to push, and an uncommitted fix reads as a run that did nothing. "+
			"If the instruction asks for something your settings deny, do the rest, commit it, and say "+
			"in your final message what you could not do and why.\n\n"+
			"Your instruction:\n\n%s", text)
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
