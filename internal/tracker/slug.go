package tracker

import (
	"fmt"
	"strings"
)

// BranchSlug is the branch name generated for a ticket: cc-<number>-<title, slugified>.
func BranchSlug(number int, title string) string {
	return fmt.Sprintf("cc-%d-%s", number, slugify(title))
}

func slugify(title string) string {
	var b strings.Builder
	atHyphen := true
	for _, r := range strings.ToLower(title) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			atHyphen = false
		case !atHyphen:
			b.WriteByte('-')
			atHyphen = true
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}
