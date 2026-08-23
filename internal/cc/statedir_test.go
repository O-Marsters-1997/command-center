package cc_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/cc"
)

func TestResolveWorkspace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	root := filepath.Join(t.TempDir(), "plain")
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, ".claude", "command-centre.toml")

	ws, err := cc.ResolveWorkspace(configPath)
	if err != nil {
		t.Fatalf("ResolveWorkspace: %v", err)
	}
	if ws.Name != "plain" {
		t.Errorf("name = %q, want %q", ws.Name, "plain")
	}
	if ws.Root != root {
		t.Errorf("root = %q, want %q", ws.Root, root)
	}
	if !strings.HasPrefix(ws.Dir, home) {
		t.Errorf("state dir %q is not under the redirected home %q", ws.Dir, home)
	}
	if want := filepath.Join(ws.Dir, "command-centre.db"); ws.DBPath != want {
		t.Errorf("db path = %q, want %q", ws.DBPath, want)
	}
	if want := filepath.Join(ws.Dir, "command-centre.lock"); ws.LockPath != want {
		t.Errorf("lock path = %q, want %q", ws.LockPath, want)
	}

	info, err := os.Stat(ws.Dir)
	if err != nil {
		t.Fatalf("state dir not created: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("state dir mode = %04o, want 0700", perm)
	}

	if want := filepath.Join(ws.Dir, "runs"); ws.RunsDir != want {
		t.Errorf("runs dir = %q, want %q", ws.RunsDir, want)
	}
	if _, err := os.Stat(ws.RunsDir); err != nil {
		t.Errorf("runs dir not created: %v", err)
	}

	if want := filepath.Join(ws.Dir, "settings", "agent.json"); ws.SettingsPath != want {
		t.Errorf("settings path = %q, want %q", ws.SettingsPath, want)
	}
	if _, err := os.Stat(filepath.Dir(ws.SettingsPath)); err != nil {
		t.Errorf("settings dir not created: %v", err)
	}
}
