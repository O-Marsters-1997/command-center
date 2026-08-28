package tracker

import "testing"

func TestBranchSlug(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		number int
		title  string
		want   string
	}{
		{name: "simple title", number: 105, title: "Eight column board", want: "cc-105-eight-column-board"},
		{name: "punctuation", number: 104, title: "Give the page a URL that names its view state!", want: "cc-104-give-the-page-a-url-that-names-its-view-state"},
		{name: "apostrophe", number: 100, title: "Put each ticket's issue title on its row", want: "cc-100-put-each-ticket-s-issue-title-on-its-row"},
		{name: "already hyphenated", number: 42, title: "status:ready-or-beyond", want: "cc-42-status-ready-or-beyond"},
		{name: "double space collapses to one hyphen", number: 7, title: "two  spaces", want: "cc-7-two-spaces"},
		{name: "leading and trailing punctuation trimmed", number: 9, title: "  --edges--  ", want: "cc-9-edges"},
		{name: "mixed case", number: 3, title: "MixedCase Title", want: "cc-3-mixedcase-title"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := BranchSlug(tt.number, tt.title)
			if got != tt.want {
				t.Errorf("BranchSlug(%d, %q) = %q, want %q", tt.number, tt.title, got, tt.want)
			}
		})
	}
}
