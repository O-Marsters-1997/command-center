package plan

import (
	"fmt"
	"strings"
)

type TicketURL string

type SHA string

type RunID int64

type BranchRef struct {
	Repo   string
	Branch string
}

func (b BranchRef) MarshalText() ([]byte, error) {
	return []byte(b.Repo + "//" + b.Branch), nil
}

func (b *BranchRef) UnmarshalText(text []byte) error {
	repo, branch, ok := strings.Cut(string(text), "//")
	if !ok {
		return fmt.Errorf("parse branch ref %q: missing //", text)
	}
	*b = BranchRef{Repo: repo, Branch: branch}
	return nil
}

func joinURLs(urls []TicketURL) string {
	parts := make([]string, len(urls))
	for i, u := range urls {
		parts[i] = string(u)
	}
	return strings.Join(parts, ", ")
}

func TicketURLs(urls []string) []TicketURL {
	out := make([]TicketURL, len(urls))
	for i, u := range urls {
		out[i] = TicketURL(u)
	}
	return out
}
