package cc_test

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/cc"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

const logTicket = "https://github.com/o/r/issues/77"

func logStreamPath(from int64) string {
	return fmt.Sprintf("/ticket/%s/log?from=%d", url.PathEscape(logTicket), from)
}

// runStore seeds one spawned, undisposed run against a log file the test appends to, and hands
// back the run id so a test can end the run mid-stream.
func runStore(t *testing.T, logPath string, now time.Time) (*cc.Store, int64) {
	t.Helper()

	ctx := t.Context()
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	ticket := cc.Ticket{URL: logTicket, Repo: "repo", Branch: "cc-77"}
	if err := store.UpsertTickets(ctx, []cc.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}
	runID, err := store.InsertRunSkeleton(ctx, ticket.URL, "agent", "basesha1234", "hash-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordSpawn(ctx, runID, 4242, now, logPath); err != nil {
		t.Fatal(err)
	}
	return store, runID
}

func appendLines(t *testing.T, path string, lines ...string) {
	t.Helper()

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	for _, line := range lines {
		if _, err := f.WriteString(line + "\n"); err != nil {
			t.Fatal(err)
		}
	}
}

// jsonToolLine is a valid stream-json line agentlog parses into a Bash tool_use event whose
// command is text, so a test can look for text in the rendered line without hand-copying markup.
func jsonToolLine(text string) string {
	return fmt.Sprintf(`{"type":"assistant","timestamp":"2026-08-20T12:00:00Z",`+
		`"message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":%q}}]}}`, text)
}

// renderedToolLine is the exact markup jsonToolLine's own event renders as, through the same
// "logline" template the production code calls.
func renderedToolLine(t *testing.T, text string) string {
	t.Helper()

	html, err := cc.RenderLogLine(agentlog.Event{Kind: agentlog.Tool, Tool: "Bash", Detail: text}, false)
	if err != nil {
		t.Fatal(err)
	}
	return string(html)
}

func endRun(t *testing.T, store *cc.Store, runID int64, at time.Time) {
	t.Helper()

	if err := store.RecordDisposition(t.Context(), runID, plan.OutcomePush, nil, at); err != nil {
		t.Fatal(err)
	}
}

// TestLogStreamsOneEventPerLine covers AC1: text/event-stream, one event per line, and the line
// escaped so an agent that logs markup cannot inject it into the page.
func TestLogStreamsOneEventPerLine(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	logPath := filepath.Join(t.TempDir(), "run.jsonl")
	first, xss, third := jsonToolLine("first"), jsonToolLine(`<script>alert(1)</script>`), jsonToolLine("third")
	appendLines(t, logPath, first, xss, third)
	store, runID := runStore(t, logPath, now)
	endRun(t, store, runID, now)
	server := cc.NewServer(store, fixedClock(now), nil, "")

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, logStreamPath(0), nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", got)
	}
	body := rec.Body.String()
	firstOffset := int64(len(first) + 1)
	for _, want := range []string{
		fmt.Sprintf("id: %d\ndata: %s\n\n", firstOffset, renderedToolLine(t, "first")),
		// Hardcoded, not built through renderedToolLine: a regression that stopped escaping
		// entirely would still make renderedToolLine "match itself".
		`data: <div class="line line-tool"><span class="line-label">tool</span> Bash ` +
			"&lt;script&gt;alert(1)&lt;/script&gt;</div>\n\n",
		"data: " + renderedToolLine(t, "third") + "\n\n",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("stream is missing %q:\n%q", want, body)
		}
	}
}

// TestLogStreamResumesFromTheOffsetTheFragmentRendered covers the seam between the detail
// fragment's static tail and the stream: the bytes the tail already printed are not resent.
func TestLogStreamResumesFromTheOffsetTheFragmentRendered(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	logPath := filepath.Join(t.TempDir(), "run.jsonl")
	alreadyRead, newOne := jsonToolLine("already read"), jsonToolLine("new one")
	appendLines(t, logPath, alreadyRead, newOne)
	store, runID := runStore(t, logPath, now)
	endRun(t, store, runID, now)
	server := cc.NewServer(store, fixedClock(now), nil, "")

	rec := httptest.NewRecorder()
	from := int64(len(alreadyRead) + 1)
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, logStreamPath(from), nil))

	body := rec.Body.String()
	if strings.Contains(body, "already read") {
		t.Errorf("stream resent a line the fragment already rendered:\n%q", body)
	}
	if want := "data: " + renderedToolLine(t, "new one") + "\n\n"; !strings.Contains(body, want) {
		t.Errorf("stream is missing the line after the offset:\n%q", body)
	}
}

// TestLogStreamIsEmptyForATicketWithNoRun covers AC2's second half: no run, and no error either.
func TestLogStreamIsEmptyForATicketWithNoRun(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	server := cc.NewServer(seededStore(t, now), fixedClock(now), nil, "")

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, logStreamPath(0), nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	if body := rec.Body.String(); body != "event: end\ndata:\n\n" {
		t.Errorf("stream for a ticket with no run is not an empty one that retires itself: %q", body)
	}
}

// TestLogStreamRetiresItselfWhenTheRunHasEnded covers the reconnect a bare close would provoke:
// EventSource reopens a dropped stream for ever, so a finished run says so on the wire.
func TestLogStreamRetiresItselfWhenTheRunHasEnded(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	logPath := filepath.Join(t.TempDir(), "run.jsonl")
	appendLines(t, logPath, "last line")
	store, runID := runStore(t, logPath, now)
	endRun(t, store, runID, now)
	server := cc.NewServer(store, fixedClock(now), nil, "")

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, logStreamPath(0), nil))

	if body := rec.Body.String(); !strings.HasSuffix(body, "event: end\ndata:\n\n") {
		t.Errorf("the stream does not end on the sentinel sse-close listens for:\n%q", body)
	}
}

// TestLogStreamResumesAReconnectFromItsLastEventID covers the other half of that reconnect: a
// browser that drops mid-run comes back with the byte it stopped on, so nothing arrives twice.
func TestLogStreamResumesAReconnectFromItsLastEventID(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	logPath := filepath.Join(t.TempDir(), "run.jsonl")
	swappedAlready, notYet := jsonToolLine("swapped already"), jsonToolLine("not yet")
	appendLines(t, logPath, swappedAlready, notYet)
	store, runID := runStore(t, logPath, now)
	endRun(t, store, runID, now)
	server := cc.NewServer(store, fixedClock(now), nil, "")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, logStreamPath(0), nil)
	req.Header.Set("Last-Event-ID", strconv.Itoa(len(swappedAlready)+1))
	server.ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "swapped already") {
		t.Errorf("a reconnect resent a line the browser had already swapped:\n%q", body)
	}
	if want := renderedToolLine(t, "not yet"); !strings.Contains(body, want) {
		t.Errorf("a reconnect skipped the line after its last event:\n%q", body)
	}
}

// TestLogStreamFollowsUntilTheRunEnds covers AC1's live half and AC2's first half: a line
// appended after the connection opened arrives, and the stream closes once ended_at is set.
func TestLogStreamFollowsUntilTheRunEnds(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	logPath := filepath.Join(t.TempDir(), "run.jsonl")
	appendLines(t, logPath, jsonToolLine("before"))
	store, runID := runStore(t, logPath, now)
	httpServer := httptest.NewServer(cc.NewServer(store, fixedClock(now), nil, ""))
	defer httpServer.Close()

	resp, err := httpServer.Client().Get(httpServer.URL + logStreamPath(0))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	events := bufio.NewReader(resp.Body)

	if got := readEvent(t, events); !strings.Contains(got, "before") {
		t.Fatalf("first event = %q, want the line already in the file", got)
	}
	appendLines(t, logPath, jsonToolLine("after"))
	if got := readEvent(t, events); !strings.Contains(got, "after") {
		t.Fatalf("second event = %q, want the line appended while connected", got)
	}

	endRun(t, store, runID, now)
	closed := make(chan error, 1)
	go func() { _, err := io.ReadAll(events); closed <- err }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("reading the closed stream: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the stream did not close when the run ended")
	}
}

// readEvent reads whole lines until the blank one that terminates an SSE event, and returns the
// event's fields joined — blocking until the server sends one.
func readEvent(t *testing.T, r *bufio.Reader) string {
	t.Helper()

	var event strings.Builder
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("reading the stream: %v", err)
		}
		if strings.TrimSpace(line) == "" {
			if event.Len() > 0 {
				return event.String()
			}
			continue
		}
		event.WriteString(line)
	}
}

// TestLogStreamLeavesTheRunAloneWhenTheClientGoesAway covers AC3: the reader owns the file and
// nothing else, so a closed tab writes no disposition and touches no pgid (invariant 9).
func TestLogStreamLeavesTheRunAloneWhenTheClientGoesAway(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	logPath := filepath.Join(t.TempDir(), "run.jsonl")
	appendLines(t, logPath, "still running")
	store, _ := runStore(t, logPath, now)
	server := cc.NewServer(store, fixedClock(now), nil, "")

	ctx, cancel := context.WithCancel(t.Context())
	req := httptest.NewRequest(http.MethodGet, logStreamPath(0), nil).WithContext(ctx)
	returned := make(chan struct{})
	go func() {
		server.ServeHTTP(httptest.NewRecorder(), req)
		close(returned)
	}()

	cancel()
	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("the handler did not return when the client went away")
	}

	runs, err := store.LatestRunsByTicket(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	run := runs[logTicket]
	if run.HasOutcome || run.EndedAt != nil {
		t.Errorf("a disconnected reader disposed of the run: outcome=%v ended=%v", run.HasOutcome, run.EndedAt)
	}
	if run.Pgid == nil || *run.Pgid != 4242 {
		t.Errorf("a disconnected reader changed the pgid: %v", run.Pgid)
	}
}

// TestDetailConnectsThePreToTheStream covers AC1's htmx half: the fragment's <pre> appends
// events, and it resumes at the byte the static tail stopped on.
func TestDetailConnectsThePreToTheStream(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	logPath := writeLog(t, 3)
	info, err := os.Stat(logPath)
	if err != nil {
		t.Fatal(err)
	}
	ticket := "https://github.com/o/r/issues/76"
	server := cc.NewServer(detailStore(t, logPath, now, now), fixedClock(now), nil, "")

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, selPagePath(ticket), nil))
	body := rec.Body.String()

	stream := fmt.Sprintf("/ticket/%s/log?from=%d", url.PathEscape(ticket), info.Size())
	for _, want := range []string{
		`hx-ext="sse"`,
		`sse-connect="` + stream + `"`,
		`sse-swap="message"`,
		`sse-close="end"`,
		`hx-swap="beforeend"`,
		`<div class="line line-tool"><span class="line-label">tool</span> Bash step 3</div>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("detail fragment is missing %q:\n%s", want, body)
		}
	}
}

// TestPageCapsThePreAtAThousandLines covers AC4, which only the browser can enforce: the page
// serves the SSE extension from the binary and drops the oldest lines past the cap.
func TestPageCapsThePreAtAThousandLines(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	server := cc.NewServer(seededStore(t, now), fixedClock(now), nil, "")

	asset := httptest.NewRecorder()
	server.ServeHTTP(asset, httptest.NewRequest(http.MethodGet, "/assets/sse.min.js", nil))
	if asset.Code != http.StatusOK {
		t.Fatalf("GET /assets/sse.min.js = %d, want 200", asset.Code)
	}

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()
	for _, want := range []string{
		`<script src="/assets/sse.min.js"></script>`,
		"htmx:sseMessage",
		"1000",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page is missing %q:\n%s", want, body)
		}
	}
}
