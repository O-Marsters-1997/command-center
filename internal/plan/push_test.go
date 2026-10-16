package plan_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestPushRefused(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		paths    []string
		policy   plan.Policy
		refused  bool
		wantPath string
	}{
		{
			name: "workflow file", paths: []string{".github/workflows/ci.yml"},
			refused: true, wantPath: ".github/workflows/ci.yml",
		},
		{name: "root mergify config", paths: []string{".mergify.yml"}, refused: true, wantPath: ".mergify.yml"},
		{name: "the repo settings file", paths: []string{plan.SettingsFile}, refused: true, wantPath: plan.SettingsFile},
		{
			name: "the repo settings file whatever the deny list says", paths: []string{plan.SettingsFile},
			policy: plan.Policy{Deny: []string{"nothing/**"}}, refused: true, wantPath: plan.SettingsFile,
		},
		{
			name: "nested package.json", paths: []string{"packages/foo/package.json"},
			refused: true, wantPath: "packages/foo/package.json",
		},
		{name: "pnpm lockfile", paths: []string{"pnpm-lock.yaml"}, refused: true, wantPath: "pnpm-lock.yaml"},
		{name: "bun lockfile", paths: []string{"web/bun.lock"}, refused: true, wantPath: "web/bun.lock"},
		{name: "npmrc", paths: []string{".npmrc"}, refused: true, wantPath: ".npmrc"},
		{
			name: "local env file", paths: []string{".env.development.local"},
			refused: true, wantPath: ".env.development.local",
		},
		{
			name: "per-repo deny addition", paths: []string{"scripts/gen.sh"},
			policy: plan.Policy{Deny: []string{"scripts/gen.sh"}}, refused: true, wantPath: "scripts/gen.sh",
		},
		{
			name: "the first hit among several paths", paths: []string{"internal/app/handler.go", ".mergify.yml", "CODEOWNERS"},
			refused: true, wantPath: ".mergify.yml",
		},
		{name: "application-only diff clears", paths: []string{"internal/app/handler.go"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			refused, path := plan.PushRefused(tt.paths, tt.policy)
			if refused != tt.refused || (refused && path != tt.wantPath) {
				t.Errorf("PushRefused(%q) = %t, %q, want %t, %q", tt.paths, refused, path, tt.refused, tt.wantPath)
			}
		})
	}
}

func TestPushPlan(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		candidates []plan.PushCandidate
		want       []plan.TicketURL
	}{
		{
			name:       "never pushed and has a tip is selected",
			candidates: []plan.PushCandidate{{URL: "sandbox://CC-1", LocalTip: "abc"}},
			want:       []plan.TicketURL{"sandbox://CC-1"},
		},
		{
			name:       "tip unchanged since the last push is not re-selected",
			candidates: []plan.PushCandidate{{URL: "sandbox://CC-1", LocalTip: "abc", LastPushedTip: "abc"}},
		},
		{
			name:       "tip moved past the last push is selected again",
			candidates: []plan.PushCandidate{{URL: "sandbox://CC-1", LocalTip: "def", LastPushedTip: "abc"}},
			want:       []plan.TicketURL{"sandbox://CC-1"},
		},
		{
			name:       "no local tip yet is never selected",
			candidates: []plan.PushCandidate{{URL: "sandbox://CC-1"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if diff := cmp.Diff(tt.want, plan.PushPlan(tt.candidates), cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("PushPlan(%+v) mismatch (-want +got):\n%s", tt.candidates, diff)
			}
		})
	}
}

func TestPRBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		base   string
		number int
		want   string
	}{
		{name: "a root PR never carries the stacking line", base: "main", number: 42, want: ""},
		{name: "no blocker PR number to reference", base: "feat-parent", number: 0, want: ""},
		{name: "a stacked PR names its blocker", base: "feat-parent", number: 42, want: "Merge after #42"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := plan.PRBody(tt.base, tt.number); got != tt.want {
				t.Errorf("PRBody(%q, %d) = %q, want %q", tt.base, tt.number, got, tt.want)
			}
		})
	}
}
