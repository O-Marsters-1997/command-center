package plan_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestBranchRef_MarshalText(t *testing.T) {
	t.Parallel()

	ref := plan.BranchRef{Repo: "acme/web", Branch: "cc-1/part"}
	got, err := ref.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText(%+v) error = %v", ref, err)
	}
	if want := "acme/web//cc-1/part"; string(got) != want {
		t.Errorf("MarshalText(%+v) = %q, want %q", ref, got, want)
	}
}

func TestBranchRef_UnmarshalText(t *testing.T) {
	t.Parallel()

	t.Run("splits at the first double slash", func(t *testing.T) {
		t.Parallel()
		var got plan.BranchRef
		if err := got.UnmarshalText([]byte("acme/web//cc-1/part")); err != nil {
			t.Fatalf("UnmarshalText(acme/web//cc-1/part) error = %v", err)
		}
		want := plan.BranchRef{Repo: "acme/web", Branch: "cc-1/part"}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("UnmarshalText(acme/web//cc-1/part) mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("a key without the separator", func(t *testing.T) {
		t.Parallel()
		var got plan.BranchRef
		if err := got.UnmarshalText([]byte("no-separator")); err == nil {
			t.Errorf("UnmarshalText(no-separator) = %+v, nil, want an error", got)
		}
	})
}
