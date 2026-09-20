package tracker

import (
	"slices"
	"testing"
)

func TestDecodeFeatures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  []byte
		want []Feature
	}{
		{
			name: "no labels",
			raw:  []byte("[]"),
			want: []Feature{},
		},
		{
			name: "only project: labels come back, nothing else",
			raw:  readFixture(t, "label_list.json"),
			want: []Feature{"project:repo-and-ticket-model", "project:fleet-view"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := decodeFeatures(tt.raw)
			if err != nil {
				t.Fatalf("decodeFeatures: %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("features = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDecodeFeaturesRejectsGarbage(t *testing.T) {
	t.Parallel()

	if _, err := decodeFeatures([]byte("not json")); err == nil {
		t.Fatal("decodeFeatures accepted non-JSON output")
	}
}

func TestTicketStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		labels []rawLabel
		want   string
	}{
		{name: "ready", labels: []rawLabel{{Name: "status:ready"}}, want: "ready"},
		{name: "backlog", labels: []rawLabel{{Name: "status:backlog"}}, want: "backlog"},
		{name: "no status label at all", labels: []rawLabel{{Name: "project:x"}}, want: ""},
		{
			name:   "status label alongside others",
			labels: []rawLabel{{Name: "project:x"}, {Name: "status:ready"}},
			want:   "ready",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := ticketStatus(tt.labels); got != tt.want {
				t.Errorf("ticketStatus(%v) = %q, want %q", tt.labels, got, tt.want)
			}
		})
	}
}

// TestDecodeBlockedBy pins issue #235: a closed dependency (98, 99, 100) is dropped, since its
// issue is already gone from the tracker's own --state open query and a stale reference to it
// only strands the dependent at blocked forever.
func TestDecodeBlockedBy(t *testing.T) {
	t.Parallel()

	got, err := decodeBlockedBy(readFixture(t, "blocked_by_105.json"))
	if err != nil {
		t.Fatalf("decodeBlockedBy: %v", err)
	}

	want := []string{
		"https://github.com/O-Marsters-1997/command-center/issues/104",
	}
	if !slices.Equal(got, want) {
		t.Errorf("blocked_by = %v, want %v", got, want)
	}
}

func TestDecodeBlockedByRejectsGarbage(t *testing.T) {
	t.Parallel()

	if _, err := decodeBlockedBy([]byte("not json")); err == nil {
		t.Fatal("decodeBlockedBy accepted non-JSON output")
	}
}
