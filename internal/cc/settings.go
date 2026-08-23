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
