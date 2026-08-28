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
