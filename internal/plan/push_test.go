package plan_test

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestPushRefused(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		path    string
		refused bool
	}{
		{name: "workflow file refuses", path: ".github/workflows/ci.yml", refused: true},
		{name: "root mergify config refuses", path: ".mergify.yml", refused: true},
		{name: "nested package.json refuses", path: "packages/foo/package.json", refused: true},
		{name: "pnpm lockfile refuses", path: "pnpm-lock.yaml", refused: true},
		{name: "bun lockfile refuses", path: "web/bun.lock", refused: true},
		{name: "npmrc refuses", path: ".npmrc", refused: true},
		{name: "local env file refuses", path: ".env.development.local", refused: true},
		{name: "per-repo deny addition refuses", path: "scripts/gen.sh", refused: true},
		{name: "application-only diff clears", path: "internal/app/handler.go", refused: false},
	}

	policy := plan.Policy{Deny: []string{"scripts/gen.sh"}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			refused, path := plan.PushRefused([]string{tt.path}, policy)
			if refused != tt.refused {
				t.Fatalf("PushRefused(%q) = %v, want %v", tt.path, refused, tt.refused)
			}
			if refused && path != tt.path {
				t.Errorf("PushRefused(%q) named path %q, want %q", tt.path, path, tt.path)
			}
		})
	}
}

func TestPushRefusedNamesTheFirstHitAmongSeveralPaths(t *testing.T) {
	t.Parallel()

	refused, path := plan.PushRefused(
		[]string{"internal/app/handler.go", ".mergify.yml", "CODEOWNERS"}, plan.Policy{})
	if !refused || path != ".mergify.yml" {
		t.Errorf("PushRefused = %v, %q, want true, \".mergify.yml\"", refused, path)
	}
}

func TestPushPlan(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		candidates []plan.PushCandidate
		want       []string
	}{
		{
			name:       "never pushed and has a tip is selected",
			candidates: []plan.PushCandidate{{TicketURL: "sandbox://CC-1", LocalTip: "abc"}},
			want:       []string{"sandbox://CC-1"},
		},
		{
			name: "tip unchanged since the last push is not re-selected",
			candidates: []plan.PushCandidate{
				{TicketURL: "sandbox://CC-1", LocalTip: "abc", LastPushedTip: "abc"},
			},
			want: nil,
		},
		{
			name: "tip moved past the last push is selected again",
			candidates: []plan.PushCandidate{
				{TicketURL: "sandbox://CC-1", LocalTip: "def", LastPushedTip: "abc"},
			},
			want: []string{"sandbox://CC-1"},
		},
		{
			name:       "no local tip yet is never selected",
			candidates: []plan.PushCandidate{{TicketURL: "sandbox://CC-1"}},
			want:       nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := plan.PushPlan(tt.candidates)
			if len(got) != len(tt.want) {
				t.Fatalf("PushPlan = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("PushPlan = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestPRBody(t *testing.T) {
	t.Parallel()

	if got := plan.PRBody("main", 42); got != "" {
		t.Errorf("PRBody(main, 42) = %q, want empty: a root PR never carries the stacking line", got)
	}
	if got := plan.PRBody("feat-parent", 0); got != "" {
		t.Errorf("PRBody(feat-parent, 0) = %q, want empty: no blocker PR number to reference", got)
	}
	if got := plan.PRBody("feat-parent", 42); got != "Merge after #42" {
		t.Errorf("PRBody(feat-parent, 42) = %q, want %q", got, "Merge after #42")
	}
}
