package config_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/config"
)

func TestResolveMCP(t *testing.T) {
	t.Parallel()

	const (
		repo = `{"mcpServers":{"twenty":{"command":"repo-twenty"}}}`
		user = `{"mcpServers":{"twenty":{"command":"user-twenty"},"codegraph":{"command":"cg"}}}`
	)

	t.Run("resolves each name, the repo before the user", func(t *testing.T) {
		t.Parallel()
		got, err := config.ResolveMCP([]string{"twenty", "codegraph"}, []byte(repo), []byte(user))
		if err != nil {
			t.Fatalf("ResolveMCP() error = %v", err)
		}
		want := `{"mcpServers":{"codegraph":{"command":"cg"},"twenty":{"command":"repo-twenty"}}}`
		if diff := cmp.Diff(want, string(got)); diff != "" {
			t.Errorf("ResolveMCP() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("no names resolve to no config", func(t *testing.T) {
		t.Parallel()
		got, err := config.ResolveMCP(nil, []byte(repo), []byte(user))
		if err != nil || got != nil {
			t.Errorf("ResolveMCP(nil) = %q, %v, want nil, nil", got, err)
		}
	})

	t.Run("a name in neither file is an error naming it", func(t *testing.T) {
		t.Parallel()
		_, err := config.ResolveMCP([]string{"ghost"}, []byte(repo), nil)
		if err == nil || !strings.Contains(err.Error(), `"ghost"`) {
			t.Errorf("ResolveMCP(ghost) error = %v, want one naming ghost", err)
		}
	})
}
