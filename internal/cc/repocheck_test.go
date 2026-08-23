package cc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readRepoSettingsFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestAssertSquashOnly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		fixture string
		wantErr string // substring, "" means no error
	}{
		{name: "squash only passes", fixture: "squash_only.json", wantErr: ""},
		{name: "allows merge commit refuses", fixture: "allows_merge_commit.json", wantErr: "allow_merge_commit"},
		{name: "allows rebase merge refuses", fixture: "allows_rebase_merge.json", wantErr: "allow_rebase_merge"},
		{name: "malformed json refuses fail-closed", fixture: "malformed_repo_settings.json", wantErr: "decode"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := assertSquashOnly("cc-sandbox", readRepoSettingsFixture(t, tt.fixture))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("assertSquashOnly: %v, want no error", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("assertSquashOnly: want error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), "cc-sandbox") {
				t.Errorf("error %q does not name the offending repo", err)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err, tt.wantErr)
			}
		})
	}
}
