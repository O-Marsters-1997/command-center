package plan

type Side string

const (
	SideOld Side = "old"
	SideNew Side = "new"
)

type ReviewComment struct {
	ID   int64
	Tip  SHA
	Path string
	Line int
	Side Side
	Hunk string // the hunk header and lines up to and including the commented line
	Body string
}
