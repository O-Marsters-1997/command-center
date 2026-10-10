package web

import (
	"bufio"
	"context"
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

func (s *Server) handleLog(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	ticketURL := r.PathValue("ticket")
	offset, _ := strconv.ParseInt(r.URL.Query().Get("from"), 10, 64)
	// A reconnecting browser sends the offset of the last event it swapped, which outranks the
	// offset the fragment was rendered with (WHATWG HTML § server-sent events).
	if resumed, err := strconv.ParseInt(r.Header.Get("Last-Event-ID"), 10, 64); err == nil {
		offset = resumed
	}
	afterRecord, _ := strconv.ParseInt(r.URL.Query().Get("records"), 10, 64)
	mode := view.NormalizeLogFilter(r.URL.Query().Get("log"))

	path, ended, err := s.store.LatestRunLog(ctx, ticketURL)
	if err != nil {
		return err
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flusher := http.NewResponseController(w)

	var tail agentlog.Tail
	for {
		sent := sendLines(w, &tail, path, &offset, mode)
		sent += s.sendRecords(ctx, w, ticketURL, &afterRecord, offset, mode)
		_ = flusher.Flush()
		if ended && sent == 0 {
			// Without a sentinel the browser treats the close as a dropped connection and
			// reconnects for ever; sse-close on the <pre> retires the EventSource on this.
			_, _ = io.WriteString(w, "event: end\ndata:\n\n")
			_ = flusher.Flush()
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-s.clock.After(logPollInterval):
		}
		if path, ended, err = s.store.LatestRunLog(ctx, ticketURL); err != nil {
			return nil
		}
	}
}

func (s *Server) sendRecords(
	ctx context.Context, w io.Writer, ticketURL string, afterID *int64, offset int64, mode string,
) int {
	records, err := s.store.TicketEventsAfter(ctx, ticketURL, *afterID)
	if err != nil {
		return 0
	}
	sent := 0
	for _, record := range records {
		*afterID = record.ID
		event := view.RecordOf(record)
		if !view.EventShown(mode, event) {
			continue
		}
		rendered, err := renderLogLine(view.LineOf(event))
		if err != nil {
			continue
		}
		if err := writeEvent(w, offset, rendered); err != nil {
			return sent
		}
		sent++
	}
	return sent
}

func sendLines(w io.Writer, tail *agentlog.Tail, path string, offset *int64, mode string) int {
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

		for _, event := range tail.Read([]byte(strings.TrimRight(line, "\r\n"))) {
			if !view.EventShown(mode, event) {
				continue
			}
			logLine := view.LineOf(event)
			logLine.Replace = event.Kind == agentlog.Cmd && event.Done && event.CallID != "" && mode != "fails"
			rendered, err := renderLogLine(logLine)
			if err != nil {
				continue
			}
			if err := writeEvent(w, *offset, rendered); err != nil {
				return sent
			}
			sent++
		}
	}
}

func writeEvent(w io.Writer, id int64, data string) error {
	var frame strings.Builder
	fmt.Fprintf(&frame, "id: %d\n", id)
	for _, line := range strings.Split(data, "\n") {
		fmt.Fprintf(&frame, "data: %s\n", line)
	}
	frame.WriteString("\n")
	_, err := io.WriteString(w, frame.String())
	return err
}
