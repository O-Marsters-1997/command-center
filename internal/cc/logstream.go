package cc

import (
	"bufio"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const logPollInterval = 250 * time.Millisecond

const (
	detailLogLines  = 50
	detailLogWindow = 64 << 10
)

// handleLog streams the run's log as Server-Sent Events, one event per whole line, from the
// ?from= byte the detail fragment's own tail stopped at. It reads the file and nothing else: the
// loop owns the agent process, so a reader arriving or leaving cannot touch it (inv. 9).
func (s *Server) handleLog(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ticketURL := r.PathValue("ticket")
	offset, _ := strconv.ParseInt(r.URL.Query().Get("from"), 10, 64)
	// A browser that reconnects sends back the byte offset of the last event it swapped, which
	// outranks the offset the fragment was rendered with (WHATWG HTML § server-sent events).
	if resumed, err := strconv.ParseInt(r.Header.Get("Last-Event-ID"), 10, 64); err == nil {
		offset = resumed
	}

	path, ended, err := s.store.LatestRunLog(ctx, ticketURL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flusher := http.NewResponseController(w)

	for {
		sent := sendLines(w, path, &offset)
		_ = flusher.Flush()
		if ended && sent == 0 {
			// Without a sentinel the browser treats the close as a dropped connection and
			// reconnects for ever; sse-close on the <pre> retires the EventSource on this.
			_, _ = io.WriteString(w, "event: end\ndata:\n\n")
			_ = flusher.Flush()
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(logPollInterval):
		}
		if path, ended, err = s.store.LatestRunLog(ctx, ticketURL); err != nil {
			return
		}
	}
}

// sendLines writes one event per whole line from offset onwards and advances offset past them,
// returning how many it wrote. A trailing partial line is left for the next read, and a log that
// will not open yet is no lines rather than an error.
func sendLines(w io.Writer, path string, offset *int64) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer func() { _ = f.Close() }()

	if _, err := f.Seek(*offset, io.SeekStart); err != nil {
		return 0
	}
	reader := bufio.NewReader(f)
	sent := 0
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return sent
		}
		*offset += int64(len(line))
		event := html.EscapeString(strings.TrimRight(line, "\r\n"))
		if _, err := fmt.Fprintf(w, "id: %d\ndata: <div>%s</div>\n\n", *offset, event); err != nil {
			return sent
		}
		sent++
	}
}

func logStreamPath(ticketURL string, from int64) string {
	return fmt.Sprintf("/ticket/%s/log?from=%d", url.PathEscape(ticketURL), from)
}

// tailLog returns the log's last lines and the byte the tail read up to, which the SSE stream
// resumes from. It is empty rather than an error when the log will not open: the agent process
// owns that file, and a ticket with no run never had one.
func tailLog(path string) ([]string, int64) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return nil, 0
	}
	// One byte behind the window, so the first break in it separates a clipped line from a whole
	// one either way and the same Cut below is right for both.
	start := max(info.Size()-detailLogWindow-1, 0)
	buf := make([]byte, info.Size()-start)
	if _, err := f.ReadAt(buf, start); err != nil {
		return nil, 0
	}

	// An agent flushes mid-line, so the tail stops at the last whole one and the stream, not the
	// fragment, carries the rest of the line it was writing.
	tail := string(buf)
	end := strings.LastIndexByte(tail, '\n')
	if end < 0 {
		return nil, start
	}
	read := start + int64(end) + 1
	tail = tail[:end]
	if start > 0 {
		_, tail, _ = strings.Cut(tail, "\n")
	}
	if tail == "" {
		return nil, read
	}
	lines := strings.Split(tail, "\n")
	if len(lines) > detailLogLines {
		lines = lines[len(lines)-detailLogLines:]
	}
	return lines, read
}
