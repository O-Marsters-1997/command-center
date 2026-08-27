package cc_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/cc"
)

func TestResolveWorkspaceLaysOutTheDataDir(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	ws, err := cc.ResolveWorkspace(dataDir)
	if err != nil {
		t.Fatalf("ResolveWorkspace: %v", err)
	}

	state := filepath.Join(dataDir, "state")
	for _, tt := range []struct{ name, got, want string }{
		{"state dir", ws.StateDir, state},
		{"repos dir", ws.ReposDir, filepath.Join(dataDir, "repos")},
		{"db path", ws.DBPath, filepath.Join(state, "command-centre.db")},
		{"lock path", ws.LockPath, filepath.Join(state, "command-centre.lock")},
		{"runs dir", ws.RunsDir, filepath.Join(state, "runs")},
		{"settings path", ws.SettingsPath, filepath.Join(state, "settings", "agent.json")},
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
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	fromEnv := t.TempDir()
	t.Setenv("CC_DATA_DIR", fromEnv)

	fromKey := t.TempDir()
	got, err := cc.ResolveDataDir(fromKey)
	if err != nil {
		t.Fatal(err)
	}
	if got != fromKey {
		t.Errorf("data dir = %q, want the config key %q", got, fromKey)
	}

	got, err = cc.ResolveDataDir("")
	if err != nil {
		t.Fatal(err)
	}
	if got != fromEnv {
		t.Errorf("data dir = %q, want CC_DATA_DIR %q", got, fromEnv)
	}

	t.Setenv("CC_DATA_DIR", "")
	got, err = cc.ResolveDataDir("")
	if err != nil {
		t.Fatal(err)
	}
	base, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(base, "command-centre"); got != want {
		t.Errorf("data dir = %q, want the default %q", got, want)
	}
}

func TestResolveDataDirExpandsHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got, err := cc.ResolveDataDir("~/cc-data")
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
	first, err := cc.ResolveWorkspace(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := cc.ResolveWorkspace(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Errorf("two resolutions of %s differ:\n%+v\n%+v", dataDir, first, second)
	}
}
