package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/config"
)

func TestResolveWorkspace(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	got, err := config.ResolveWorkspace(dataDir)
	if err != nil {
		t.Fatalf("ResolveWorkspace(%s) error = %v", dataDir, err)
	}

	state := filepath.Join(dataDir, "state")
	want := config.Workspace{
		DataDir:          dataDir,
		StateDir:         state,
		ReposDir:         filepath.Join(dataDir, "repos"),
		LockPath:         filepath.Join(state, "command-centre.lock"),
		RunsDir:          filepath.Join(state, "runs"),
		SettingsPath:     filepath.Join(state, "settings", "agent.json"),
		SystemPromptPath: filepath.Join(state, "settings", "system-prompt.md"),
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("ResolveWorkspace(%s) mismatch (-want +got):\n%s", dataDir, diff)
	}

	for _, dir := range []string{got.StateDir, got.ReposDir, got.RunsDir, filepath.Dir(got.SettingsPath)} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Errorf("ResolveWorkspace(%s) did not create %s: %v", dataDir, dir, err)
			continue
		}
		if perm := info.Mode().Perm(); perm != 0o700 {
			t.Errorf("ResolveWorkspace(%s) made %s with mode %04o, want 0700", dataDir, dir, perm)
		}
	}
}

func TestResolveDataDir(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	envDir := t.TempDir()
	keyDir := t.TempDir()
	withEnv := config.Env{Home: home, UserConfigDir: filepath.Join(home, ".config"), DataDir: envDir}
	noEnv := config.Env{Home: home, UserConfigDir: filepath.Join(home, ".config")}

	tests := []struct {
		name       string
		configured string
		env        config.Env
		want       string
	}{
		{name: "the config key wins over the environment", configured: keyDir, env: withEnv, want: keyDir},
		{name: "CC_DATA_DIR when the key is unset", env: withEnv, want: envDir},
		{name: "the default under the user config dir", env: noEnv, want: filepath.Join(home, ".config", "command-centre")},
		{name: "a leading ~ expands to home", configured: "~/cc-data", env: noEnv, want: filepath.Join(home, "cc-data")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := config.ResolveDataDir(tt.configured, tt.env)
			if err != nil {
				t.Fatalf("ResolveDataDir(%q) error = %v", tt.configured, err)
			}
			if got != tt.want {
				t.Errorf("ResolveDataDir(%q) = %q, want %q", tt.configured, got, tt.want)
			}
		})
	}
}

func TestEnvFrom(t *testing.T) {
	t.Parallel()

	t.Run("reads the named variables", func(t *testing.T) {
		t.Parallel()
		vars := map[string]string{
			"HOME": "/home/olly", "XDG_CONFIG_HOME": "/xdg", "CC_DATA_DIR": "/data",
			"CC_DATABASE_URL": "postgres://x", "CC_AGENT_COMMAND": `["a"]`, "TZ": "Europe/Paris",
		}
		got, err := config.EnvFrom(func(key string) string { return vars[key] })
		if err != nil {
			t.Fatalf("EnvFrom(vars) error = %v", err)
		}
		want := config.Env{
			DataDir: "/data", DatabaseURL: "postgres://x", AgentCommand: `["a"]`,
			Home: "/home/olly", UserConfigDir: "/xdg", TZ: "Europe/Paris",
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("EnvFrom(vars) mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("a missing HOME is an error", func(t *testing.T) {
		t.Parallel()
		if _, err := config.EnvFrom(func(string) string { return "" }); err == nil {
			t.Error("EnvFrom(no HOME) error = nil, want one")
		}
	})
}
