package gh

import (
	"maps"
	"testing"
)

func TestDecodeIssueTitles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  []byte
		want map[string]string
	}{
		{
			name: "no issues",
			raw:  []byte("[]"),
			want: map[string]string{},
		},
		{
			name: "keyed on the issue url, not the number",
			raw:  readFixture(t, "issue_list.json"),
			want: map[string]string{
				"https://github.com/owner/repo/issues/100": "Put each ticket's issue title on its row",
				"https://github.com/owner/repo/issues/101": "Give the board its own route",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := decodeIssueTitles(tt.raw)
			if err != nil {
				t.Fatalf("decodeIssueTitles: %v", err)
			}
			if !maps.Equal(got, tt.want) {
				t.Errorf("titles = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDecodeIssueTitlesRejectsGarbage(t *testing.T) {
	t.Parallel()

	if _, err := decodeIssueTitles([]byte("not json")); err == nil {
		t.Fatal("decodeIssueTitles accepted non-JSON output")
	}
}
