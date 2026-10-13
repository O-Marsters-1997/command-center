package loop

import (
	"errors"
	"fmt"
	"os"

	"github.com/O-Marsters-1997/command-center/internal/config"
)

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

// agentSystemPrompt is appended to every spawned agent's own system prompt via
// --append-system-prompt-file: a Task-spawned subagent's completion notification is delivered
// into a later turn, and claude -p has none to deliver it into.
const agentSystemPrompt = `This session runs non-interactively (claude -p). Each run ends with your turn, and the
owner may continue the session later with a message.

Foreground subagents (Task, where you wait on the result within this turn) are allowed.
Do not spawn a background subagent and end your turn waiting for its result: its completion
notification arrives in a later turn, and this session has none. That result never reaches you,
and the process exits with your work uncommitted.

Do the writing, editing and committing yourself.

Write a comment only if it passes this test: name the specific thing a reader would get wrong
without it, and that thing must live outside this repo's control. If you cannot name it, or the
answer is your own code being confusing, delete the comment and fix the code. The only exceptions:
legal or licence headers, a directive that changes what the build does (//go:build, //go:generate),
a constraint imposed from outside the repo (vendor, protocol, spec, upstream bug — link it), and a
doc comment on an exported identifier stating its contract.

The shell is zsh. Prefer rg over grep --include for searching.

Commit before your turn ends.
`

func WriteAgentFiles(ws config.Workspace) error {
	var errs []error
	for _, f := range []struct{ path, content string }{
		{ws.SettingsPath, agentSettings},
		{ws.SystemPromptPath, agentSystemPrompt},
	} {
		if err := os.WriteFile(f.path, []byte(f.content), 0o600); err != nil {
			errs = append(errs, fmt.Errorf("write agent file %s: %w", f.path, err))
		}
	}
	return errors.Join(errs...)
}
