package plan_test

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestSeamChanged(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		c    plan.SeamCheck
		want bool
	}{
		{
			name: "unauthorised, no run: nothing was ever consented to",
			c:    plan.SeamCheck{},
			want: false,
		},
		{
			name: "queued, hash still matches the authorised member's",
			c:    plan.SeamCheck{Authorised: true, ComposeOK: true, ComposedHash: "h1", MemberHash: "h1"},
			want: false,
		},
		{
			name: "queued, hash no longer matches the authorised member's",
			c:    plan.SeamCheck{Authorised: true, ComposeOK: true, ComposedHash: "h1", MemberHash: "h2"},
			want: true,
		},
		{
			name: "queued, a seam no longer resolves at all",
			c:    plan.SeamCheck{Authorised: true, ComposeOK: false, MemberHash: "h1"},
			want: true,
		},
		{
			name: "already run, hash still matches that run's own stored hash",
			c:    plan.SeamCheck{HasRun: true, ComposeOK: true, ComposedHash: "h1", RunHash: "h1"},
			want: false,
		},
		{
			name: "already run, hash no longer matches that run's own stored hash",
			c:    plan.SeamCheck{HasRun: true, ComposeOK: true, ComposedHash: "h1", RunHash: "h2"},
			want: true,
		},
		{
			name: "already run, a seam no longer resolves at all",
			c:    plan.SeamCheck{HasRun: true, ComposeOK: false, RunHash: "h1"},
			want: true,
		},
		{
			name: "a run outranks membership: compares against the run's hash, not the member's stale one",
			c: plan.SeamCheck{
				HasRun: true, Authorised: true, ComposeOK: true, ComposedHash: "h1", RunHash: "h1", MemberHash: "h2",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := plan.SeamChanged(tt.c); got != tt.want {
				t.Errorf("SeamChanged() = %v, want %v", got, tt.want)
			}
		})
	}
}
