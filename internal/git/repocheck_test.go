package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func readRepoSettingsFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestAssertRepoSettings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		fixture    string
		wantKind   string
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

			kind, reason, err := assertRepoSettings("acme/cc", readRepoSettingsFixture(t, tt.fixture))
			if err != nil {
				t.Fatalf("assertRepoSettings: %v", err)
			}
			if kind != tt.wantKind {
				t.Errorf("kind = %q, want %q", kind, tt.wantKind)
			}
			if !strings.Contains(reason, tt.wantReason) {
				t.Errorf("reason %q does not contain %q", reason, tt.wantReason)
			}
		})
	}
}

func TestAssertRepoSettingsFailsClosedOnMalformedJSON(t *testing.T) {
	t.Parallel()

	if _, _, err := assertRepoSettings("acme/cc", readRepoSettingsFixture(t, "malformed_repo_settings.json")); err == nil {
		t.Error("assertRepoSettings(malformed) = nil error, want one")
	}
}
