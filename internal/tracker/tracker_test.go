package tracker_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/command"
	"github.com/O-Marsters-1997/command-center/internal/gh"
	"github.com/O-Marsters-1997/command-center/internal/tracker"
)

func TestResolve(t *testing.T) {
	t.Parallel()

	t.Run("a GitHub source reads the remote's owner and repo", func(t *testing.T) {
		t.Parallel()
		var got []string
		run := func(_ context.Context, _ time.Duration, _, _ string, args ...string) ([]byte, error) {
			got = args
			return []byte("[]"), nil
		}
		src, err := tracker.Resolve(gh.CLI{Exec: command.ExecutorFunc(run)})(tracker.GitHub, remote)
		if err != nil {
			t.Fatalf("Resolve(GitHub, %q) error = %v", remote, err)
		}
		if _, err := src.Features(t.Context()); err != nil {
			t.Fatalf("Features() error = %v", err)
		}
		i := slices.Index(got, "-R")
		if i < 0 || got[i+1] != "O-Marsters-1997/command-center" {
			t.Errorf("Features() ran gh %v, want -R O-Marsters-1997/command-center", got)
		}
	})

	t.Run("errors", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name   string
			kind   tracker.Kind
			remote string
		}{
			{name: "unimplemented kind", kind: "linear", remote: "github.com/owner/repo"},
			{name: "empty remote", kind: tracker.GitHub, remote: ""},
			{name: "owner only", kind: tracker.GitHub, remote: "owner-only"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				if _, err := tracker.Resolve(gh.CLI{})(tt.kind, tt.remote); err == nil {
					t.Errorf("Resolve()(%q, %q) error = nil, want one", tt.kind, tt.remote)
				}
			})
		}
	})
}
