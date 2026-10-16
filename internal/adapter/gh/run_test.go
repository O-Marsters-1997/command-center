package gh_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/adapter/gh"
)

func TestDecode(t *testing.T) {
	t.Parallel()

	if _, err := gh.Decode[gh.RepoSettings]([]byte("not json")); err == nil {
		t.Error("Decode(not json) error = nil, want one")
	}
}

func TestDecodeStream(t *testing.T) {
	t.Parallel()

	t.Run("concatenates pages", func(t *testing.T) {
		t.Parallel()
		got, err := gh.DecodeStream[int]([]byte(`[1,2][3]`))
		if err != nil {
			t.Fatalf("DecodeStream() error = %v", err)
		}
		if diff := cmp.Diff([]int{1, 2, 3}, got); diff != "" {
			t.Errorf("DecodeStream([1,2][3]) mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("empty output", func(t *testing.T) {
		t.Parallel()
		if _, err := gh.DecodeStream[int](nil); err == nil {
			t.Error("DecodeStream(nil) error = nil, want one")
		}
	})
}
