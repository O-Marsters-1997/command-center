package gh

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestDecodeFailsOnMalformedJSON(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.Join("testdata", "malformed_repo_settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode[RepoSettings](raw); err == nil {
		t.Error("Decode(malformed) = nil error, want one")
	}
}

func TestDecodeStreamConcatenatesPages(t *testing.T) {
	t.Parallel()

	got, err := DecodeStream[int]([]byte(`[1,2][3]`))
	if err != nil {
		t.Fatalf("DecodeStream() error = %v", err)
	}
	if want := []int{1, 2, 3}; !slices.Equal(got, want) {
		t.Errorf("DecodeStream() = %v, want %v", got, want)
	}
}

func TestDecodeStreamRejectsEmptyOutput(t *testing.T) {
	t.Parallel()

	if _, err := DecodeStream[int](nil); err == nil {
		t.Error("DecodeStream(nil) = nil error, want one")
	}
}
