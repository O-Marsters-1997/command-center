package plan

type Side string

const (
	SideOld Side = "old"
	SideNew Side = "new"
)

const ReviewAttachmentName = "review-comments.json"

type ReviewComment struct {
	ID   int64  `json:"-"`
	Tip  SHA    `json:"tip"`
	Path string `json:"path"`
	Line int    `json:"line"`
	Side Side   `json:"side"`
	Hunk string `json:"hunk"` // the hunk header and lines up to and including the commented line
	Body string `json:"body"`
}
