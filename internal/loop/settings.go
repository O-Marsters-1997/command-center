package loop

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/O-Marsters-1997/command-center/internal/adapter/git"
	"github.com/O-Marsters-1997/command-center/internal/config"
)

func resolveMCP(ctx context.Context, checkout, userConfig string, names []string) ([]byte, error) {
	if len(names) == 0 {
		return nil, nil
	}
	var repoBody []byte
	found, err := git.HasFile(ctx, checkout, config.SettingsRef, config.MCPFile)
	if err != nil {
		return nil, fmt.Errorf("look for %s on %s: %w", config.MCPFile, config.SettingsRef, err)
	}
	if found {
		text, err := git.ShowFile(ctx, checkout, config.SettingsRef, config.MCPFile)
		if err != nil {
			return nil, fmt.Errorf("read %s on %s: %w", config.MCPFile, config.SettingsRef, err)
		}
		repoBody = []byte(text)
	}
	userBody, err := os.ReadFile(userConfig)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("read %s: %w", userConfig, err)
	}
	body, err := config.ResolveMCP(names, repoBody, userBody)
	if err != nil {
		return nil, fmt.Errorf("resolve mcp servers %v: %w", names, err)
	}
	return body, nil
}

func (l *Loop) mcpConfigFor(ctx context.Context, runID int64, repo string) (string, error) {
	checkout := l.checkout(repo)
	settings, _, err := readRepoSettings(ctx, checkout)
	if err != nil {
		return "", fmt.Errorf("read settings for run %d: %w", runID, err)
	}
	body, err := resolveMCP(ctx, checkout, l.cfg.ClaudeUserConfig, settings.MCP)
	if err != nil {
		return "", fmt.Errorf("resolve mcp servers for run %d: %w", runID, err)
	}
	if body == nil {
		return "", nil
	}
	path := filepath.Join(l.ws.RunsDir, fmt.Sprintf("%d.mcp.json", runID))
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return "", fmt.Errorf("write mcp config for run %d: %w", runID, err)
	}
	return path, nil
}

const agentSettings = `{
  "permissions": {
    "deny": [
      "Bash(git push:*)",
      "Bash(gh:*)",
      "WebFetch",
      "WebSearch"
    ]
  },
  "enabledPlugins": {
    "gopls-lsp@claude-plugins-official": false
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

The gh CLI is denied in this session, so do not try it. Work from the repository and git.

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
