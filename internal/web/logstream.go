package web

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

const logPollInterval = 250 * time.Millisecond

var errClientGone = errors.New("client went away")

func (s *Server) handleLog(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	ticketURL := r.PathValue("ticket")
	offset, err := queryOffset(r, "from")
	if err != nil {
		return fmt.Errorf("load query offset: %w", err)
	}
	// Last-Event-ID takes precedence on reconnect (WHATWG HTML § server-sent events).
	if resumed, err := strconv.ParseInt(r.Header.Get("Last-Event-ID"), 10, 64); err == nil && resumed >= 0 {
		offset = resumed
	}
	afterRecord, err := queryOffset(r, "records")
	if err != nil {
		return fmt.Errorf("load query offset: %w", err)
	}
	mode := view.ParseLogFilter(r.URL.Query().Get("log"))

	path, ended, err := s.latestRunLog(ctx, ticketURL)
	if err != nil {
		return fmt.Errorf("load latest run log: %w", err)
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flusher := http.NewResponseController(w)

	follow := logFollow{
		TicketURL: ticketURL, Path: path, Ended: ended, Offset: offset, AfterRecord: afterRecord, Mode: mode,
	}
	err = s.followLog(ctx, w, flusher, follow)
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

type logFollow struct {
	TicketURL   string
	Path        string
	Ended       bool
	Offset      int64
	AfterRecord int64
	Mode        view.LogFilter
}

func (s *Server) followLog(ctx context.Context, w io.Writer, flusher *http.ResponseController, f logFollow) error {
	var tail agentlog.Tail
	for {
		sent, err := sendLines(w, &tail, &f)
		if err != nil {
			return fmt.Errorf("send lines: %w", err)
		}
		records, err := s.sendRecords(ctx, w, &f)
		if err != nil {
			return fmt.Errorf("send records: %w", err)
		}
		sent += records
		if err := flushStream(flusher); err != nil {
			return fmt.Errorf("flush stream: %w", err)
		}
		if f.Ended && sent == 0 {
			// EventSource reconnects when the server closes without an explicit end event.
			return writeEnd(w, flusher)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("follow run log: %w", ctx.Err())
		case <-time.After(logPollInterval):
		}
		if f.Path, f.Ended, err = s.latestRunLog(ctx, f.TicketURL); err != nil {
			return fmt.Errorf("load latest run log: %w", err)
		}
	}
}

func (s *Server) latestRunLog(ctx context.Context, ticketURL string) (path string, ended bool, err error) {
	path, ended, err = s.store.LatestRunLog(ctx, ticketURL)
	if errors.Is(err, store.ErrNotFound) {
		return "", true, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read latest run log for %s: %w", ticketURL, err)
	}
	return path, ended, nil
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
	if err == nil {
		return nil
	}
	if errors.Is(err, http.ErrNotSupported) {
		return fmt.Errorf("flush stream: %w", err)
	}
	return fmt.Errorf("%w: %w", errClientGone, err)
}

func (s *Server) sendRecords(ctx context.Context, w io.Writer, f *logFollow) (int, error) {
	sent := 0
	err := view.EachTicketEvent(ctx, s.store, f.TicketURL, f.AfterRecord, func(record store.Event) error {
		f.AfterRecord = record.ID
		event := view.RecordOf(record)
		if !view.RecordShown(record) || !view.EventShown(f.Mode, event) {
			return nil
		}
		rendered, err := renderLogLine(view.LineOf(event))
		if err != nil {
			return fmt.Errorf("render record %d: %w", record.ID, err)
		}
		if err := writeEvent(w, f.Offset, string(rendered)); err != nil {
			return fmt.Errorf("write event: %w", err)
		}
		sent++
		return nil
	})
	if err != nil {
		return sent, fmt.Errorf("read ticket events for %s: %w", f.TicketURL, err)
	}
	return sent, nil
}

func sendLines(w io.Writer, tail *agentlog.Tail, follow *logFollow) (sent int, err error) {
	path, mode := follow.Path, follow.Mode
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

	if _, err := f.Seek(follow.Offset, io.SeekStart); err != nil {
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
		follow.Offset += int64(len(line))

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
			if err := writeEvent(w, follow.Offset, string(rendered)); err != nil {
				return sent, fmt.Errorf("write event: %w", err)
			}
			sent++
		}
	}
}

func writeEvent(w io.Writer, id int64, data string) error {
	var frame strings.Builder
	fmt.Fprintf(&frame, "id: %d\n", id)
	for line := range strings.SplitSeq(data, "\n") {
		fmt.Fprintf(&frame, "data: %s\n", line)
	}
	frame.WriteString("\n")
	if _, err := io.WriteString(w, frame.String()); err != nil {
		return fmt.Errorf("%w: %w", errClientGone, err)
	}
	return nil
}

func renderLogLine(line view.LogLine) (template.HTML, error) {
	var buf strings.Builder
	if err := templates.ExecuteTemplate(&buf, "logline", line); err != nil {
		return "", fmt.Errorf("render log line: %w", err)
	}
	return template.HTML(buf.String()), nil
}
