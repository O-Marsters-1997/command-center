package gh

import (
	"errors"
	"maps"
	"strings"
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

func TestDecodeIssueTitlesFullPageErrors(t *testing.T) {
	t.Parallel()

	raw := "[" + strings.Repeat(`{"title":"t","url":"u"},`, ListLimit-1) + `{"title":"t","url":"v"}]`
	_, err := decodeIssueTitles([]byte(raw))
	if !errors.Is(err, ErrFullPage) {
		t.Fatalf("decodeIssueTitles() error = %v, want ErrFullPage", err)
	}
}

func TestDecodeRejectsUnknownStateAndBadMergedAt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
	}{
		{name: "unknown state", raw: `[{"number":1,"state":"WEIRD"}]`},
		{name: "malformed mergedAt", raw: `[{"number":1,"state":"MERGED","mergedAt":"yesterday"}]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := decode([]byte(tt.raw)); err == nil {
				t.Errorf("decode(%s) accepted it", tt.raw)
			}
		})
	}
}

func TestDecodeAcceptsEmptyMergedAt(t *testing.T) {
	t.Parallel()

	got, err := decode([]byte(`[{"number":1,"state":"OPEN","mergedAt":""}]`))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 || !got[0].MergedAt.IsZero() {
		t.Errorf("decode = %+v, want one PR with zero MergedAt", got)
	}
}
