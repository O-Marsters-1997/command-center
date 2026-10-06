package web

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
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
	mode := view.NormalizeLogFilter(r.URL.Query().Get("log"))

	path, ended, err := s.store.LatestRunLog(ctx, ticketURL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flusher := http.NewResponseController(w)

	for {
		sent := sendLines(w, path, &offset, mode)
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
		case <-s.clock.After(logPollInterval):
		}
		if path, ended, err = s.store.LatestRunLog(ctx, ticketURL); err != nil {
			return
		}
	}
}

// sendLines writes one SSE event per whole line from offset onwards through renderLogLine, and
// returns how many it sent. A trailing partial line is left for the next read, and a log that
// will not open yet is no lines rather than an error.
func sendLines(w io.Writer, path string, offset *int64, mode string) int {
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

		event, ok := agentlog.ParseLine([]byte(strings.TrimRight(line, "\r\n")))
		if !ok || !view.KindShown(mode, event.Kind) {
			continue
		}
		// ponytail: a failure streamed in live never carries id="first-fail", even when it's the
		// run's first -- the anchor only lands on the next full re-render. Upgrade by tracking
		// whether a Fail has already crossed this connection, once that gap is worth closing.
		rendered, err := renderLogLine(event, false)
		if err != nil {
			continue
		}
		if _, err := fmt.Fprintf(w, "id: %d\ndata: %s\n\n", *offset, rendered); err != nil {
			return sent
		}
		sent++
	}
}
