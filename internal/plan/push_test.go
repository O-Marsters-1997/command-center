package plan_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestClassify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		paths      []string
		policy     plan.Policy
		want       []plan.PathMatch
		wantPushed bool
	}{
		{
			name: "workflow file is protected by default", paths: []string{".github/workflows/x.yaml"},
			want:       []plan.PathMatch{{Path: ".github/workflows/x.yaml", Rule: ".github/**", Tier: plan.TierProtected}},
			wantPushed: true,
		},
		{
			name: "protect list naming a default changes nothing", paths: []string{".github/workflows/x.yaml"},
			policy:     plan.Policy{Protect: []string{".github/**"}},
			want:       []plan.PathMatch{{Path: ".github/workflows/x.yaml", Rule: ".github/**", Tier: plan.TierProtected}},
			wantPushed: true,
		},
		{
			name: "mergify config is denied", paths: []string{".mergify.yml"},
			want: []plan.PathMatch{{Path: ".mergify.yml", Rule: ".mergify.yml", Tier: plan.TierDenied}},
		},
		{
			name: "env file is denied", paths: []string{".env.local"},
			want: []plan.PathMatch{{Path: ".env.local", Rule: ".env*", Tier: plan.TierDenied}},
		},
		{
			name: "protect moves a default deny to protected", paths: []string{".npmrc"},
			policy:     plan.Policy{Protect: []string{".npmrc"}},
			want:       []plan.PathMatch{{Path: ".npmrc", Rule: ".npmrc", Tier: plan.TierProtected}},
			wantPushed: true,
		},
		{
			name: "the settings file stays denied whatever protect says", paths: []string{plan.SettingsFile},
			policy: plan.Policy{Protect: []string{plan.SettingsFile}},
			want:   []plan.PathMatch{{Path: plan.SettingsFile, Rule: plan.SettingsFile, Tier: plan.TierDenied}},
		},
		{
			name: "a path in both lists is denied", paths: []string{"packages/foo/package.json"},
			policy: plan.Policy{Deny: []string{"packages/foo/package.json"}},
			want: []plan.PathMatch{
				{Path: "packages/foo/package.json", Rule: "packages/foo/package.json", Tier: plan.TierDenied},
			},
		},
		{
			name: "matches keep input order", paths: []string{"a.go", "bun.lock", ".npmrc", "CODEOWNERS"},
			want: []plan.PathMatch{
				{Path: "bun.lock", Rule: "**/bun.lock", Tier: plan.TierProtected},
				{Path: ".npmrc", Rule: ".npmrc", Tier: plan.TierDenied},
				{Path: "CODEOWNERS", Rule: "CODEOWNERS", Tier: plan.TierProtected},
			},
		},
		{name: "application-only diff matches nothing", paths: []string{"internal/app/handler.go"}, wantPushed: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := plan.Classify(tt.paths, tt.policy)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Classify(%q) mismatch (-want +got):\n%s", tt.paths, diff)
			}
			if pushed := len(plan.Denied(got)) == 0; pushed != tt.wantPushed {
				t.Errorf("Classify(%q) pushes = %t, want %t", tt.paths, pushed, tt.wantPushed)
			}
			if n := len(plan.Denied(got)) + len(plan.Protected(got)); n != len(got) {
				t.Errorf("Denied + Protected of %v = %d matches, want %d", got, n, len(got))
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
