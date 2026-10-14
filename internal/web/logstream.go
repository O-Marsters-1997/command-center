package web

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

const logPollInterval = 250 * time.Millisecond

var errClientGone = errors.New("client went away")

func (s *Server) handleLog(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	ticketURL := r.PathValue("ticket")
	offset, err := queryOffset(r, "from")
	if err != nil {
		return err
	}
	// A reconnecting browser sends the offset of the last event it swapped, which outranks the
	// offset the fragment was rendered with (WHATWG HTML § server-sent events).
	if resumed, err := strconv.ParseInt(r.Header.Get("Last-Event-ID"), 10, 64); err == nil && resumed >= 0 {
		offset = resumed
	}
	afterRecord, err := queryOffset(r, "records")
	if err != nil {
		return err
	}
	mode := view.ParseLogFilter(r.URL.Query().Get("log"))

	path, ended, err := s.store.LatestRunLog(ctx, ticketURL)
	if err != nil {
		return err
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flusher := http.NewResponseController(w)

	err = s.followLog(ctx, w, flusher, ticketURL, path, ended, offset, afterRecord, mode)
	switch {
	case err == nil, errors.Is(err, errClientGone), errors.Is(err, context.Canceled):
		return nil
	}
	s.logger.Warn("stream ticket log", "ticket", ticketURL, "err", err)
	_, err = io.WriteString(w, "event: error\ndata: the log could not be read\n\n")
	if err == nil {
		err = writeEnd(w, flusher)
	}
	if errors.Is(err, errClientGone) {
		return nil
	}
	return err
}

func (s *Server) followLog(
	ctx context.Context, w io.Writer, flusher *http.ResponseController,
	ticketURL, path string, ended bool, offset, afterRecord int64, mode view.LogFilter,
) error {
	var tail agentlog.Tail
	for {
		sent, err := sendLines(w, &tail, path, &offset, mode)
		if err != nil {
			return err
		}
		records, err := s.sendRecords(ctx, w, ticketURL, &afterRecord, offset, mode)
		if err != nil {
			return err
		}
		sent += records
		if err := flushStream(flusher); err != nil {
			return err
		}
		if ended && sent == 0 {
			// Without a sentinel the browser treats the close as a dropped connection and
			// reconnects for ever; sse-close on the <pre> retires the EventSource on this.
			return writeEnd(w, flusher)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(logPollInterval):
		}
		if path, ended, err = s.store.LatestRunLog(ctx, ticketURL); err != nil {
			return err
		}
	}
}

func queryOffset(r *http.Request, key string) (int64, error) {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 0 {
		return 0, errorf(http.StatusBadRequest, "%s %q is not a non-negative integer", key, raw)
	}
	return n, nil
}

func writeEnd(w io.Writer, flusher *http.ResponseController) error {
	if _, err := io.WriteString(w, "event: end\ndata:\n\n"); err != nil {
		return fmt.Errorf("%w: %w", errClientGone, err)
	}
	return flushStream(flusher)
}

func flushStream(flusher *http.ResponseController) error {
	err := flusher.Flush()
	switch {
	case err == nil, errors.Is(err, http.ErrNotSupported):
		return err
	}
	return fmt.Errorf("%w: %w", errClientGone, err)
}

func (s *Server) sendRecords(
	ctx context.Context, w io.Writer, ticketURL string, afterID *int64, offset int64, mode view.LogFilter,
) (int, error) {
	records, err := s.store.TicketEventsAfter(ctx, ticketURL, *afterID)
	if err != nil {
		return 0, err
	}
	sent := 0
	for _, record := range records {
		*afterID = record.ID
		event := view.RecordOf(record)
		if !view.RecordShown(record) || !view.EventShown(mode, event) {
			continue
		}
		rendered, err := renderLogLine(view.LineOf(event))
		if err != nil {
			return sent, fmt.Errorf("render record %d: %w", record.ID, err)
		}
		if err := writeEvent(w, offset, rendered); err != nil {
			return sent, err
		}
		sent++
	}
	return sent, nil
}

func sendLines(
	w io.Writer, tail *agentlog.Tail, path string, offset *int64, mode view.LogFilter,
) (sent int, err error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("open log %s: %w", path, err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close log %s: %w", path, closeErr))
		}
	}()

	if _, err := f.Seek(*offset, io.SeekStart); err != nil {
		return 0, fmt.Errorf("seek log %s: %w", path, err)
	}
	reader := bufio.NewReader(f)
	for {
		line, err := reader.ReadString('\n')
		if errors.Is(err, io.EOF) {
			return sent, nil
		}
		if err != nil {
			return sent, fmt.Errorf("read log %s: %w", path, err)
		}
		*offset += int64(len(line))

		for _, event := range tail.Read([]byte(strings.TrimRight(line, "\r\n"))) {
			if !view.EventShown(mode, event) {
				continue
			}
			logLine := view.LineOf(event)
			isCommand := event.Kind == agentlog.Cmd || event.Kind == agentlog.Commit
			logLine.Replace = isCommand && event.Done && event.CallID != "" && mode != view.LogFails
			rendered, err := renderLogLine(logLine)
			if err != nil {
				return sent, fmt.Errorf("render log line: %w", err)
			}
			if err := writeEvent(w, *offset, rendered); err != nil {
				return sent, err
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
	if _, err := io.WriteString(w, frame.String()); err != nil {
		return fmt.Errorf("%w: %w", errClientGone, err)
	}
	return nil
}
