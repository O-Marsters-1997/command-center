package gh

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", name, err)
	}
	return raw
}

func decodeFixture(t *testing.T, fixture string) []PR {
	t.Helper()
	prs, err := decode(readFixture(t, fixture))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return prs
}

func TestDecodeNoPRs(t *testing.T) {
	t.Parallel()
	if got := decodeFixture(t, "no_prs.json"); len(got) != 0 {
		t.Errorf("decode(no_prs) = %+v, want no PRs", got)
	}
}

func TestDecodeRollup(t *testing.T) {
	t.Parallel()
	prs := decodeFixture(t, "mixed_rollup.json")
	if len(prs) != 1 {
		t.Fatalf("decode(mixed_rollup) = %+v, want one PR", prs)
	}
	pr := prs[0]
	if pr.State != Open || pr.Number != 41 || pr.HeadRef != "cc-1-first" {
		t.Errorf("decode(mixed_rollup) PR = %+v, want open PR 41 on cc-1-first", pr)
	}
	if _, ok := pr.Checks[""]; ok {
		t.Error("decode(mixed_rollup) kept a nameless check")
	}
	if len(pr.Checks) != 2 {
		t.Errorf("decode(mixed_rollup) checks = %v, want Tests and ci/legacy", pr.Checks)
	}
	if tests, ok := pr.Checks["Tests"]; !ok {
		t.Errorf("decode(mixed_rollup) checks = %v, want Tests", pr.Checks)
	} else {
		if tests.Conclusion != "SUCCESS" {
			t.Errorf("Tests conclusion = %q, want SUCCESS", tests.Conclusion)
		}
		if want := time.Date(2026, 8, 20, 10, 10, 0, 0, time.UTC); !tests.StartedAt.Equal(want) {
			t.Errorf("Tests startedAt = %s, want %s", tests.StartedAt, want)
		}
	}
	if legacy, ok := pr.Checks["ci/legacy"]; !ok || legacy.Conclusion != "SUCCESS" {
		t.Errorf("ci/legacy = %+v, present %v; want SUCCESS", legacy, ok)
	}
	if !pr.MergedAt.IsZero() {
		t.Errorf("open PR mergedAt = %s, want zero", pr.MergedAt)
	}
}

func TestDecodeEmptyRollup(t *testing.T) {
	t.Parallel()
	prs := decodeFixture(t, "empty_rollup.json")
	if len(prs) != 1 {
		t.Fatalf("decode(empty_rollup) = %+v, want one PR", prs)
	}
	if len(prs[0].Checks) != 0 || !prs[0].IsDraft {
		t.Errorf("decode(empty_rollup) = %+v, want draft with no checks", prs[0])
	}
}

func TestDecodeMerged(t *testing.T) {
	t.Parallel()
	prs := decodeFixture(t, "merged.json")
	if len(prs) != 1 {
		t.Fatalf("decode(merged) = %+v, want one PR", prs)
	}
	want := time.Date(2026, 8, 20, 10, 15, 0, 0, time.UTC)
	if prs[0].State != Merged || !prs[0].MergedAt.Equal(want) {
		t.Errorf("decode(merged) = %+v, want merged at %s", prs[0], want)
	}
}

func TestDecodeLabels(t *testing.T) {
	t.Parallel()
	prs := decodeFixture(t, "stacked_ready_to_merge.json")
	if len(prs) != 1 {
		t.Fatalf("decode(stacked_ready_to_merge) = %+v, want one PR", prs)
	}
	want := []string{"keep-open", "ready-to-merge"}
	if diff := cmp.Diff(want, prs[0].Labels); diff != "" {
		t.Errorf("decode() labels mismatch (-want +got):\n%s", diff)
	}
}

func TestBulkAndFallbackFieldsBothRequestLabels(t *testing.T) {
	t.Parallel()

	if !strings.Contains(bulkFields, "labels") {
		t.Errorf("bulkFields = %q, want it to include labels", bulkFields)
	}
	if !strings.Contains(fallbackFields, "labels") {
		t.Errorf("fallbackFields = %q, want it to include labels", fallbackFields)
	}
}

func TestBulkAndFallbackFieldsBothRequestMergedAt(t *testing.T) {
	t.Parallel()

	if !strings.Contains(bulkFields, "mergedAt") {
		t.Errorf("bulkFields = %q, want it to include mergedAt", bulkFields)
	}
	if !strings.Contains(fallbackFields, "mergedAt") {
		t.Errorf("fallbackFields = %q, want it to include mergedAt", fallbackFields)
	}
}

func TestPRStateAbsentIsTheZeroValue(t *testing.T) {
	t.Parallel()

	var s PRState
	if s != Absent {
		t.Errorf("zero PRState = %v, want Absent: absence must be a value, not a nil pointer", s)
	}
	if s.String() != "absent" {
		t.Errorf("String() = %q, want %q", s.String(), "absent")
	}
}
