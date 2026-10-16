package loop

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/gittest"
)

func TestReadRepoSettings(t *testing.T) {
	t.Parallel()

	t.Run("reads origin/main, never a branch or the working tree", func(t *testing.T) {
		t.Parallel()
		checkout, _ := gittest.WithOrigin(t)
		gittest.Commit(t, checkout, config.SettingsFile, "deny = [\"go.mod\"]\n")
		gittest.Run(t, checkout, "push", "-q", "origin", "main")
		gittest.Run(t, checkout, "switch", "-q", "-c", "ticket")
		path := filepath.Join(checkout, config.SettingsFile)
		if err := os.WriteFile(path, []byte("deny = []\n"), 0o600); err != nil {
			t.Fatalf("WriteFile(%s): %v", path, err)
		}
		gittest.Run(t, checkout, "commit", "-q", "-am", "loosen")

		got, source, err := readRepoSettings(t.Context(), checkout)
		if err != nil {
			t.Fatalf("readRepoSettings() error = %v", err)
		}
		want := config.RepoSettings{Tracker: "github", Deny: []string{"go.mod"}}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("readRepoSettings() mismatch (-want +got):\n%s", diff)
		}
		if source != config.SourceFile {
			t.Errorf("readRepoSettings() source = %q, want %q", source, config.SourceFile)
		}
	})
}
