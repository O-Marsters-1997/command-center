package tracker

import (
	"slices"
	"testing"
)

func TestDecodeGroups(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  []byte
		want []Group
	}{
		{
			name: "no labels",
			raw:  []byte("[]"),
			want: []Group{},
		},
		{
			name: "only project: labels come back, nothing else",
			raw:  readFixture(t, "label_list.json"),
			want: []Group{"project:repo-and-ticket-model", "project:fleet-view"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := decodeGroups(tt.raw)
			if err != nil {
				t.Fatalf("decodeGroups: %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("groups = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDecodeGroupsRejectsGarbage(t *testing.T) {
	t.Parallel()

	if _, err := decodeGroups([]byte("not json")); err == nil {
		t.Fatal("decodeGroups accepted non-JSON output")
	}
}

func TestInFlightStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		labels     []rawLabel
		wantStatus string
		wantOK     bool
	}{
		{name: "ready", labels: []rawLabel{{Name: "status:ready"}}, wantStatus: "ready", wantOK: true},
		{name: "in-progress", labels: []rawLabel{{Name: "status:in-progress"}}, wantStatus: "in-progress", wantOK: true},
		{name: "in-review", labels: []rawLabel{{Name: "status:in-review"}}, wantStatus: "in-review", wantOK: true},
		{name: "done", labels: []rawLabel{{Name: "status:done"}}, wantStatus: "done", wantOK: true},
		{name: "backlog is not in flight", labels: []rawLabel{{Name: "status:backlog"}}, wantOK: false},
		{name: "no status label at all is not in flight", labels: []rawLabel{{Name: "project:x"}}, wantOK: false},
		{
			name:       "status label alongside others",
			labels:     []rawLabel{{Name: "project:x"}, {Name: "status:ready"}},
			wantStatus: "ready",
			wantOK:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, ok := inFlightStatus(tt.labels)
			if ok != tt.wantOK || status != tt.wantStatus {
				t.Errorf("inFlightStatus(%v) = %q, %v; want %q, %v", tt.labels, status, ok, tt.wantStatus, tt.wantOK)
			}
		})
	}
}

func TestDecodeIssuesFiltersOnStatus(t *testing.T) {
	t.Parallel()

	issues, err := decodeIssues(readFixture(t, "issue_list_ready_and_beyond.json"))
	if err != nil {
		t.Fatalf("decodeIssues: %v", err)
	}

	var inFlight []int
	for _, issue := range issues {
		if _, ok := inFlightStatus(issue.Labels); ok {
			inFlight = append(inFlight, issue.Number)
		}
	}

	want := []int{120, 121}
	if !slices.Equal(inFlight, want) {
		t.Errorf("in-flight issue numbers = %v, want %v (a status:backlog or unlabelled issue leaked through)", inFlight, want)
	}
}

func TestDecodeBlockedBy(t *testing.T) {
	t.Parallel()

	got, err := decodeBlockedBy(readFixture(t, "blocked_by_105.json"))
	if err != nil {
		t.Fatalf("decodeBlockedBy: %v", err)
	}

	want := []string{
		"https://github.com/O-Marsters-1997/command-center/issues/98",
		"https://github.com/O-Marsters-1997/command-center/issues/99",
		"https://github.com/O-Marsters-1997/command-center/issues/100",
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
