package gh

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestDecode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		fixture string
		want    func(*testing.T, []PR)
	}{
		{
			name:    "no prs",
			fixture: "no_prs.json",
			want: func(t *testing.T, prs []PR) {
				if len(prs) != 0 {
					t.Fatalf("prs = %d, want 0", len(prs))
				}
			},
		},
		{
			name:    "one name five times reduces to the latest completed run",
			fixture: "mixed_rollup.json",
			want: func(t *testing.T, prs []PR) {
				if len(prs) != 1 {
					t.Fatalf("prs = %d, want 1", len(prs))
				}
				pr := prs[0]
				if pr.State != Open || pr.Number != 41 || pr.HeadRef != "cc-1-first" {
					t.Errorf("pr = %+v", pr)
				}
				tests, ok := pr.Checks["Tests"]
				if !ok {
					t.Fatalf("no Tests check in %v", pr.Checks)
				}
				if tests.Conclusion != "SUCCESS" {
					t.Errorf("conclusion = %q, want SUCCESS (the latest COMPLETED run)", tests.Conclusion)
				}
				if want := time.Date(2026, 8, 20, 10, 10, 0, 0, time.UTC); !tests.StartedAt.Equal(want) {
					t.Errorf("startedAt = %s, want %s", tests.StartedAt, want)
				}
			},
		},
		{
			name:    "a status context keeps its context as its name",
			fixture: "mixed_rollup.json",
			want: func(t *testing.T, prs []PR) {
				got, ok := prs[0].Checks["ci/legacy"]
				if !ok {
					t.Fatalf("no ci/legacy check in %v", prs[0].Checks)
				}
				if got.Conclusion != "SUCCESS" {
					t.Errorf("conclusion = %q, want SUCCESS", got.Conclusion)
				}
			},
		},
		{
			name:    "a nameless entry is dropped, not panicked on",
			fixture: "mixed_rollup.json",
			want: func(t *testing.T, prs []PR) {
				if _, ok := prs[0].Checks[""]; ok {
					t.Error("a nameless rollup entry was kept under the empty name")
				}
				if len(prs[0].Checks) != 2 {
					t.Errorf("checks = %v, want exactly Tests and ci/legacy", prs[0].Checks)
				}
			},
		},
		{
			name:    "an empty rollup is empty, not green",
			fixture: "empty_rollup.json",
			want: func(t *testing.T, prs []PR) {
				if len(prs[0].Checks) != 0 {
					t.Errorf("checks = %v, want none", prs[0].Checks)
				}
				if !prs[0].IsDraft {
					t.Error("isDraft was dropped")
				}
			},
		},
		{
			name:    "merged state survives the fallback read",
			fixture: "merged.json",
			want: func(t *testing.T, prs []PR) {
				if prs[0].State != Merged {
					t.Errorf("state = %v, want Merged", prs[0].State)
				}
			},
		},
		{
			name:    "labels decode by name",
			fixture: "stacked_ready_to_merge.json",
			want: func(t *testing.T, prs []PR) {
				want := []string{"keep-open", "ready-to-merge"}
				if !slices.Equal(prs[0].Labels, want) {
					t.Errorf("labels = %v, want %v", prs[0].Labels, want)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			prs, err := decode(readFixture(t, tt.fixture))
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			tt.want(t, prs)
		})
	}
}

// TestBulkAndFallbackFieldsBothRequestLabels guards the gap the fallback read almost carried: a
// PR resolved only through it (merged, closed, or past the bulk page's own limit) must still
// decode ready-to-merge, since invariant 2's warning reads whatever labels the PR actually has
// (docs/designs/command-centre-design.md § 4a inv. 2).
func TestBulkAndFallbackFieldsBothRequestLabels(t *testing.T) {
	t.Parallel()

	if !strings.Contains(bulkFields, "labels") {
		t.Errorf("bulkFields = %q, want it to include labels", bulkFields)
	}
	if !strings.Contains(fallbackFields, "labels") {
		t.Errorf("fallbackFields = %q, want it to include labels", fallbackFields)
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
