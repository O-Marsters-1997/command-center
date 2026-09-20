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
Do not spawn a background subagent (e.g. via the Task tool) and end your turn waiting for its
result: its completion notification arrives in a later turn, and this session has none. That
result never reaches you, and the process exits with your work uncommitted.
Do any work, including code review, yourself within this turn, and commit before it ends.
`

// WriteAgentSystemPrompt writes the default system prompt to path. Idempotent: the content never
// varies by call, so writing it again (e.g. on every App.New()) is a no-op in effect.
func WriteAgentSystemPrompt(path string) error {
	if err := os.WriteFile(path, []byte(agentSystemPrompt), 0o600); err != nil {
		return fmt.Errorf("write agent system prompt %s: %w", path, err)
	}
	return nil
}
