package git_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/command"
	"github.com/O-Marsters-1997/command-center/internal/gh"
	"github.com/O-Marsters-1997/command-center/internal/git"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func ghReturning(out []byte, err error) gh.CLI {
	run := func(context.Context, time.Duration, string, string, ...string) ([]byte, error) { return out, err }
	return gh.CLI{Exec: command.ExecutorFunc(run)}
}

func TestCheckRepo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		fixture    string
		wantKind   plan.RefusalKind
		wantReason string
	}{
		{name: "squash only on main passes", fixture: "squash_only.json"},
		{
			name: "merge commits refuse", fixture: "allows_merge_commit.json",
			wantKind: plan.RefusalMergeSettings, wantReason: "allow_merge_commit",
		},
		{
			name: "rebase merges refuse", fixture: "allows_rebase_merge.json",
			wantKind: plan.RefusalMergeSettings, wantReason: "allow_rebase_merge",
		},
		{
			name: "a master default refuses with the rename hint", fixture: "master_default.json",
			wantKind: plan.RefusalDefaultBranch, wantReason: "rename it on GitHub",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			raw, err := os.ReadFile(filepath.Join("testdata", tt.fixture))
			if err != nil {
				t.Fatalf("ReadFile(%s): %v", tt.fixture, err)
			}

			kind, reason, err := git.CheckRepo(t.Context(), ghReturning(raw, nil), "acme/cc")
			if err != nil {
				t.Fatalf("CheckRepo(%s) error = %v", tt.fixture, err)
			}
			if kind != tt.wantKind || !strings.Contains(reason, tt.wantReason) {
				t.Errorf("CheckRepo(%s) = %q, %q, want %q and a reason naming %q", tt.fixture, kind, reason, tt.wantKind, tt.wantReason)
			}
		})
	}

	t.Run("a GitHub failure is an error, not a pass", func(t *testing.T) {
		t.Parallel()
		errGH := errors.New("gh exited 1")
		if _, _, err := git.CheckRepo(t.Context(), ghReturning(nil, errGH), "acme/cc"); !errors.Is(err, errGH) {
			t.Errorf("CheckRepo(gh fails) error = %v, want %v", err, errGH)
		}
	})
}
