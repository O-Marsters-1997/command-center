package loop_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/adapter/git/gittest"
	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/loop"
)

func writeAgentFiles(t *testing.T) config.Workspace {
	t.Helper()
	dir := t.TempDir()
	ws := config.Workspace{
		SettingsPath:     filepath.Join(dir, "agent.json"),
		SystemPromptPath: filepath.Join(dir, "system-prompt.md"),
	}
	if err := loop.WriteAgentFiles(ws); err != nil {
		t.Fatalf("WriteAgentFiles: %v", err)
	}
	return ws
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", path, err)
	}
	return raw
}

func TestLaunchMCPConfig(t *testing.T) {
	t.Parallel()

	const mcpJSON = `{"mcpServers":{"twenty":{"command":"node"}}}`
	push := func(t *testing.T, f *loopFixture, settings string) {
		t.Helper()
		repoPath := config.CheckoutPath(f.Root, "repo")
		gittest.Commit(t, repoPath, config.MCPFile, mcpJSON)
		pushSettingsFile(t, repoPath, settings)
	}

	t.Run("an empty allowlist spawns with no mcp config", func(t *testing.T) {
		t.Parallel()
		f := newLoopFixture(t)
		push(t, f, "mcp = []\n")
		f.AuthoriseAll(t)
		f.Tick(t)

		spawns := f.Fake.Spawns()
		if len(spawns) != 1 || spawns[0].MCPConfigPath != "" {
			t.Errorf("spawns = %+v, want one with no MCPConfigPath", spawns)
		}
	})

	t.Run("a named server is written to the run's mcp config", func(t *testing.T) {
		t.Parallel()
		f := newLoopFixture(t)
		push(t, f, "mcp = [\"twenty\"]\n")
		f.AuthoriseAll(t)
		f.Tick(t)

		spawns := f.Fake.Spawns()
		if len(spawns) != 1 {
			t.Fatalf("spawns = %d, want 1", len(spawns))
		}
		if diff := cmp.Diff(mcpJSON, string(readFile(t, spawns[0].MCPConfigPath))); diff != "" {
			t.Errorf("mcp config mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("a server in neither file stops the run from starting", func(t *testing.T) {
		t.Parallel()
		f := newLoopFixture(t)
		push(t, f, "mcp = [\"ghost\"]\n")
		f.AuthoriseAll(t)
		if err := f.Loop.RunOnce(t.Context()); err == nil {
			t.Error("RunOnce() error = nil, want the unresolved mcp server")
		}
		if spawns := f.Fake.Spawns(); len(spawns) != 0 {
			t.Errorf("spawns = %+v, want none", spawns)
		}
	})
}

func TestWriteAgentFiles(t *testing.T) {
	t.Parallel()

	t.Run("settings deny push, gh and network fetch", func(t *testing.T) {
		t.Parallel()
		raw := readFile(t, writeAgentFiles(t).SettingsPath)
		assertGolden(t, "testdata/settings.golden.json", raw)

		var settings struct {
			Permissions struct {
				Deny []string `json:"deny"`
			} `json:"permissions"`
		}
		if err := json.Unmarshal(raw, &settings); err != nil {
			t.Fatalf("decode settings: %v\n%s", err, raw)
		}
		want := []string{"Bash(git push:*)", "Bash(gh:*)", "WebFetch", "WebSearch"}
		if diff := cmp.Diff(want, settings.Permissions.Deny); diff != "" {
			t.Errorf("settings deny mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("system prompt", func(t *testing.T) {
		t.Parallel()
		assertGolden(t, "testdata/system-prompt.golden.txt", readFile(t, writeAgentFiles(t).SystemPromptPath))
	})

	t.Run("is idempotent", func(t *testing.T) {
		t.Parallel()
		ws := writeAgentFiles(t)
		first := readFile(t, ws.SettingsPath)
		if err := loop.WriteAgentFiles(ws); err != nil {
			t.Fatalf("second WriteAgentFiles: %v", err)
		}
		if diff := cmp.Diff(string(first), string(readFile(t, ws.SettingsPath))); diff != "" {
			t.Errorf("settings across two writes mismatch (-want +got):\n%s", diff)
		}
	})
}
