package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

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

type RedCheck struct {
	Name       string
	DetailsURL string
}

func ComposeFixCI(checks []RedCheck) string {
	var b strings.Builder
	b.WriteString("CI is failing on this branch. Fix the failing checks:\n\n")
	for _, c := range checks {
		fmt.Fprintf(&b, "- %s", c.Name)
		if c.DetailsURL != "" {
			fmt.Fprintf(&b, ": %s", c.DetailsURL)
		}
		b.WriteString("\n")
	}
	b.WriteString("\nReproduce each failure locally, fix its cause and commit.")
	return b.String()
}

func ComposeFixVerify(tail string) string {
	return fmt.Sprintf("The verify command failed on this branch's tip. Fix what it reports, "+
		"re-run it locally and commit. Its output ended:\n\n```\n%s\n```", strings.TrimRight(tail, "\n"))
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

const ciLogLines = 200

func ComposeCILog(check, log string) string {
	lines := strings.Split(strings.TrimRight(log, "\n"), "\n")
	lines = lines[max(0, len(lines)-ciLogLines):]
	return fmt.Sprintf("## Failed CI log: %s\n\nLast %d lines of the failed log:\n\n```\n%s\n```",
		check, ciLogLines, strings.Join(lines, "\n"))
}

func ComposeCILogUnavailable(check string) string {
	return fmt.Sprintf("## Failed CI log: %s\n\n"+
		"The failed log could not be retrieved. Treat the CI failure as unverified: you have not seen the log.", check)
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
	case OutcomePush, OutcomeFailed, OutcomeCutFailed, OutcomePassed:
	}
	return fmt.Sprintf("The owner's follow-up instruction:\n\n%s", text)
}

type ResolveBrief struct {
	Title, BaseTitle, BaseBranch string
	At                           ConflictAt
	Paths                        []string
}

const ResolveCommitPrefix = "resolve:"

func ComposeResolve(b ResolveBrief) string {
	paths := "- " + strings.Join(b.Paths, "\n- ")
	return fmt.Sprintf(
		"A refresh merged `origin/%[3]s` into this branch and left the worktree mid-merge with conflicts. "+
			"Finish that merge.\n\n"+
			"This branch carries the ticket %[1]q at %[4]s. The base carries %[2]q at %[5]s.\n\n"+
			"Conflicted paths:\n%[6]s\n\n"+
			"Resolve each path so both sides' intent survives, run the checks the repo defines for what you "+
			"touched, stage the result and run `git commit`. Start the commit subject with %[7]q. "+
			"Do not abort the merge, rebase or force anything, and do not push: the daemon that spawned you "+
			"verifies the commit and pushes it. Your settings deny `git push` and `gh`.",
		b.Title, b.BaseTitle, b.BaseBranch, b.At.BranchTip, b.At.BaseTip, paths, ResolveCommitPrefix+" ")
}

func Hash(composed string) string {
	sum := sha256.Sum256([]byte(composed))
	return hex.EncodeToString(sum[:])
}
