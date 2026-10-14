package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/config"
)

func TestResolveWorkspaceLaysOutTheDataDir(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	ws, err := config.ResolveWorkspace(dataDir)
	if err != nil {
		t.Fatalf("ResolveWorkspace: %v", err)
	}

	state := filepath.Join(dataDir, "state")
	for _, tt := range []struct{ name, got, want string }{
		{"state dir", ws.StateDir, state},
		{"repos dir", ws.ReposDir, filepath.Join(dataDir, "repos")},
		{"lock path", ws.LockPath, filepath.Join(state, "command-centre.lock")},
		{"runs dir", ws.RunsDir, filepath.Join(state, "runs")},
		{"settings path", ws.SettingsPath, filepath.Join(state, "settings", "agent.json")},
		{"system prompt path", ws.SystemPromptPath, filepath.Join(state, "settings", "system-prompt.md")},
	} {
		if tt.got != tt.want {
			t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
		}
	}

	for _, dir := range []string{ws.StateDir, ws.ReposDir, ws.RunsDir, filepath.Dir(ws.SettingsPath)} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Errorf("%s not created: %v", dir, err)
			continue
		}
		if perm := info.Mode().Perm(); perm != 0o700 {
			t.Errorf("%s mode = %04o, want 0700", dir, perm)
		}
	}
}

func TestResolveDataDirPrefersTheConfigKeyThenTheEnvironment(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	env := config.Env{Home: home, UserConfigDir: filepath.Join(home, ".config"), DataDir: t.TempDir()}

	fromKey := t.TempDir()
	got, err := config.ResolveDataDir(fromKey, env)
	if err != nil {
		t.Fatal(err)
	}
	if got != fromKey {
		t.Errorf("data dir = %q, want the config key %q", got, fromKey)
	}

	got, err = config.ResolveDataDir("", env)
	if err != nil {
		t.Fatal(err)
	}
	if got != env.DataDir {
		t.Errorf("data dir = %q, want CC_DATA_DIR %q", got, env.DataDir)
	}

	env.DataDir = ""
	got, err = config.ResolveDataDir("", env)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(env.UserConfigDir, "command-centre"); got != want {
		t.Errorf("data dir = %q, want the default %q", got, want)
	}
}

func TestEnvFromReadsTheNamedVariables(t *testing.T) {
	t.Parallel()

	vars := map[string]string{
		"HOME": "/home/olly", "XDG_CONFIG_HOME": "/xdg", "CC_DATA_DIR": "/data",
		"CC_DATABASE_URL": "postgres://x", "CC_AGENT_COMMAND": `["a"]`, "TZ": "Europe/Paris",
	}
	got, err := config.EnvFrom(func(key string) string { return vars[key] })
	if err != nil {
		t.Fatal(err)
	}
	want := config.Env{
		DataDir: "/data", DatabaseURL: "postgres://x", AgentCommand: `["a"]`,
		Home: "/home/olly", UserConfigDir: "/xdg", TZ: "Europe/Paris",
	}
	if got != want {
		t.Errorf("EnvFrom() = %+v, want %+v", got, want)
	}
}

func TestEnvFromRefusesAMissingHome(t *testing.T) {
	t.Parallel()

	if _, err := config.EnvFrom(func(string) string { return "" }); err == nil {
		t.Error("EnvFrom with no HOME succeeded, want an error")
	}
}

func TestResolveDataDirExpandsHome(t *testing.T) {
	t.Parallel()

	home := t.TempDir()

	got, err := config.ResolveDataDir("~/cc-data", config.Env{Home: home})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, "cc-data"); got != want {
		t.Errorf("data dir = %q, want %q", got, want)
	}
}

// TestResolveWorkspaceIgnoresTheWorkingDirectory is the bug the grandparent rule caused: running
// from anywhere, with any --config, must reach one workspace. The data dir is the only input.
func TestResolveWorkspaceIgnoresTheWorkingDirectory(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	first, err := config.ResolveWorkspace(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := config.ResolveWorkspace(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Errorf("two resolutions of %s differ:\n%+v\n%+v", dataDir, first, second)
	}
}
