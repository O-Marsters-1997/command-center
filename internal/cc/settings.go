package cc

import (
	"fmt"
	"os"
)

// agentSettings is the app-owned settings file passed to every spawn: deny beats the repos'
// own tracked and synced allows, so an agent's pre-approved push (docs/prds/prd-command-centre.md §
// The agent edits the CI config) never reaches it.
const agentSettings = `{
  "permissions": {
    "deny": [
      "Bash(git push:*)",
      "Bash(gh:*)",
      "WebFetch",
      "WebSearch"
    ]
  }
}
`

// WriteAgentSettings writes the static deny settings to path. Idempotent: the content never
// varies by call, so writing it again (e.g. on every App.New()) is a no-op in effect.
func WriteAgentSettings(path string) error {
	if err := os.WriteFile(path, []byte(agentSettings), 0o600); err != nil {
		return fmt.Errorf("write agent settings %s: %w", path, err)
	}
	return nil
}

// agentSystemPrompt is appended to every spawned agent's own system prompt via
// --append-system-prompt-file: a Task-spawned subagent's completion notification is delivered
// into a later turn, and claude -p has none to deliver it into.
const agentSystemPrompt = `This session is single-shot: it runs non-interactively (claude -p) and will not resume.

Foreground subagents (Task, where you wait on the result within this turn) are allowed.
Do not spawn a background subagent and end your turn waiting for its result: its completion
notification arrives in a later turn, and this session has none. That result never reaches you,
and the process exits with your work uncommitted.

Delegate read-heavy, low-output work — searching, grepping, or reading several files to answer
one question — to the digest subagent, in the foreground. It runs on a smaller model with a
narrower tool set and returns roughly 1k tokens, so that reading never lands in your own context.
Do the writing, editing and committing yourself; code review also stays in this run for now.

Write a comment only if it passes this test: name the specific thing a reader would get wrong
without it, and that thing must live outside this repo's control. If you cannot name it, or the
answer is your own code being confusing, delete the comment and fix the code. The only exceptions:
legal or licence headers, a directive that changes what the build does (//go:build, //go:generate),
a constraint imposed from outside the repo (vendor, protocol, spec, upstream bug — link it), and a
doc comment on an exported identifier stating its contract.

The shell is zsh. Prefer rg over grep --include for searching.

Commit before your turn ends.
`

// WriteAgentSystemPrompt writes the default system prompt to path. Idempotent: the content never
// varies by call, so writing it again (e.g. on every App.New()) is a no-op in effect.
func WriteAgentSystemPrompt(path string) error {
	if err := os.WriteFile(path, []byte(agentSystemPrompt), 0o600); err != nil {
		return fmt.Errorf("write agent system prompt %s: %w", path, err)
	}
	return nil
}

// agentDigestDefinition is the --agents JSON passed to every spawned implement run.
const agentDigestDefinition = `{
  "digest": {
    "description": "Reads code or output to answer one question, without growing the caller's context.",
    "prompt": "If the repo root has a .codegraph/ directory, start with one codegraph explore \"<symbols or question>\" in Bash and treat the source it prints as already read. Read only what it takes to answer. Reply with the answer, at most 1000 tokens, no preamble.",
    "tools": [
      "Read",
      "Grep",
      "Glob",
      "Bash"
    ],
    "model": "haiku"
  }
}
`

// WriteAgentDigestDefinition writes the digest subagent definition to path. Idempotent: the
// content never varies by call, so writing it again (e.g. on every App.New()) is a no-op in
// effect.
func WriteAgentDigestDefinition(path string) error {
	if err := os.WriteFile(path, []byte(agentDigestDefinition), 0o600); err != nil {
		return fmt.Errorf("write agent digest definition %s: %w", path, err)
	}
	return nil
}
