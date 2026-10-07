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

func (s *Server) handleLog(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ticketURL := r.PathValue("ticket")
	offset, _ := strconv.ParseInt(r.URL.Query().Get("from"), 10, 64)
	// A reconnecting browser sends the offset of the last event it swapped, which outranks the
	// offset the fragment was rendered with (WHATWG HTML § server-sent events).
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
