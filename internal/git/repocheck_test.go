package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/gh"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func readRepoSettingsFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", name, err)
	}
	return raw
}

func TestAssertRepoSettings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		fixture    string
		wantKind   plan.RefusalKind
		wantReason string
	}{
		{name: "squash only on main passes", fixture: "squash_only.json"},
		{name: "merge commits refuse", fixture: "allows_merge_commit.json",
			wantKind: plan.RefusalMergeSettings, wantReason: "allow_merge_commit"},
		{name: "rebase merges refuse", fixture: "allows_rebase_merge.json",
			wantKind: plan.RefusalMergeSettings, wantReason: "allow_rebase_merge"},
		{name: "a master default refuses with the rename hint", fixture: "master_default.json",
			wantKind:   plan.RefusalDefaultBranch,
			wantReason: "default branch is `master`, not `main`; rename it on GitHub, then Track again."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			settings, err := gh.Decode[gh.RepoSettings](readRepoSettingsFixture(t, tt.fixture))
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			kind, reason := assertRepoSettings("acme/cc", settings)
			if kind != tt.wantKind {
				t.Errorf("kind = %q, want %q", kind, tt.wantKind)
			}
			if !strings.Contains(reason, tt.wantReason) {
				t.Errorf("reason %q does not contain %q", reason, tt.wantReason)
			}
		})
	}
}
