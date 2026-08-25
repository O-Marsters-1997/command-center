package cc

import (
	_ "embed"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"strings"
)

//go:embed detail.tmpl
var detailSource string

var detailFragment = template.Must(page.New("detail").Parse(detailSource))

const (
	detailLogLines  = 50
	detailLogWindow = 64 << 10
)

type detailView struct {
	row
	LogTail string
}

func (s *Server) handleDetail(w http.ResponseWriter, r *http.Request) {
	view, err := s.render(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	taskURL := r.PathValue("task")
	target, ok := findRow(view, taskURL)
	if !ok {
		http.Error(w, fmt.Sprintf("unknown task %q", taskURL), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	detail := detailView{row: target, LogTail: tailLog(target.LogPath)}
	if err := detailFragment.Execute(w, detail); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// tailLog is empty rather than an error when the log will not open: the agent process owns that
// file, and a task with no run never had one.
func tailLog(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return ""
	}
	// One byte behind the window, so the first break in it separates a clipped line from a whole
	// one either way and the same Cut below is right for both.
	start := max(info.Size()-detailLogWindow-1, 0)
	buf := make([]byte, info.Size()-start)
	if _, err := f.ReadAt(buf, start); err != nil {
		return ""
	}

	tail := strings.TrimRight(string(buf), "\n")
	if start > 0 {
		_, tail, _ = strings.Cut(tail, "\n")
	}
	lines := strings.Split(tail, "\n")
	if len(lines) > detailLogLines {
		lines = lines[len(lines)-detailLogLines:]
	}
	return strings.Join(lines, "\n")
}
