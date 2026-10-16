package web_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/htmltest"
	"github.com/O-Marsters-1997/command-center/internal/option"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
)

const logTicket = "https://github.com/o/r/issues/77"

func logStreamPath(from int64) string {
	return fmt.Sprintf("/ticket/%s/log?from=%d", url.PathEscape(logTicket), from)
}

func runStore(t *testing.T, logPath string, now time.Time) (*storepkg.Store, int64) {
	t.Helper()

	store := openStore(t)
	ticket := storepkg.Ticket{URL: logTicket, Repo: "repo", Branch: "cc-77"}
	storetest.Tickets(t, store, ticket)
	runID, err := store.InsertRunSkeleton(t.Context(), storepkg.RunSkeleton{
		Ticket: ticket.URL, Kind: "agent", PromptHash: "hash-1",
	})
	if err != nil {
		t.Fatalf("InsertRunSkeleton(%s): %v", ticket.URL, err)
	}
	if err := store.RecordSpawn(t.Context(), storepkg.Spawn{
		RunID: plan.RunID(runID), Pgid: 4242, StartedAt: now, LogPath: logPath,
	}); err != nil {
		t.Fatalf("RecordSpawn(%d): %v", runID, err)
	}
	return store, runID
}

func appendLines(t *testing.T, path string, lines ...string) {
	t.Helper()

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("OpenFile(%s): %v", path, err)
	}
	defer func() { _ = f.Close() }()
	for _, line := range lines {
		if _, err := f.WriteString(line + "\n"); err != nil {
			t.Fatalf("WriteString(%s): %v", path, err)
		}
	}
}

func jsonToolLine(text string) string {
	return fmt.Sprintf(`{"type":"assistant","timestamp":"2026-08-20T12:00:00Z",`+
		`"message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":%q}}]}}`, text)
}

type sseEvent struct {
	Name string
	ID   string
	Data []string
}

func (e sseEvent) HTML() string { return strings.Join(e.Data, "\n") }

func parseSSE(body string) []sseEvent {
	var events []sseEvent
	for _, frame := range strings.Split(body, "\n\n") {
		if frame == "" {
			continue
		}
		var e sseEvent
		for _, line := range strings.Split(frame, "\n") {
			field, value, _ := strings.Cut(line, ":")
			value = strings.TrimPrefix(value, " ")
			switch field {
			case "event":
				e.Name = value
			case "id":
				e.ID = value
			case "data":
				e.Data = append(e.Data, value)
			}
		}
		events = append(events, e)
	}
	return events
}

func messageEvents(events []sseEvent) []sseEvent {
	var out []sseEvent
	for _, e := range events {
		if e.Name == "" {
			out = append(out, e)
		}
	}
	return out
}

func callArgs(t *testing.T, events []sseEvent) []string {
	t.Helper()

	var args []string
	for _, e := range messageEvents(events) {
		for _, arg := range htmltest.Parse(t, e.HTML()).Find(".call-arg") {
			args = append(args, arg.Text())
		}
	}
	return args
}

func streamedFrom(t *testing.T, logPath string, query string) []sseEvent {
	t.Helper()

	now := testNow
	store, runID := runStore(t, logPath, now)
	endRun(t, store, runID, now)
	rec := do(t, newServer(store, now), http.MethodGet, logStreamPath(0)+query, req{})
	if rec.Code != http.StatusOK {
		t.Fatalf("GET log stream = %d, want 200: %s", rec.Code, rec.Body)
	}
	return parseSSE(rec.Body)
}

func endRun(t *testing.T, store *storepkg.Store, runID int64, at time.Time) {
	t.Helper()

	if err := store.RecordDisposition(t.Context(), storepkg.Disposition{
		RunID: plan.RunID(runID), Outcome: plan.OutcomePush, ExitCode: option.None[int](),
		Cost: option.None[float64](), EndedAt: at,
	}); err != nil {
		t.Fatalf("RecordDisposition(%d): %v", runID, err)
	}
}

func TestLogStream(t *testing.T) {
	t.Parallel()

	for name, run := range map[string]func(*testing.T){
		"streams one event per line":                  testLogStreamsOneEventPerLine,
		"applies the log filter":                      testLogStreamFilter,
		"carries multi-line output as one event":      testLogStreamMultiLineOutput,
		"resumes from the offset the fragment drew":   testLogStreamResumesFromOffset,
		"is empty for a ticket with no run":           testLogStreamEmptyWithoutRun,
		"sends an error frame when the log is unread": testLogStreamErrorFrame,
		"refuses a malformed offset":                  testLogStreamMalformedOffset,
		"retires itself when the run has ended":       testLogStreamRetiresWhenRunEnded,
		"resumes a reconnect from Last-Event-ID":      testLogStreamResumesFromLastEventID,
		"follows until the run ends":                  testLogStreamFollowsUntilRunEnds,
		"leaves the run alone when the client leaves": testLogStreamLeavesRunAlone,
		"is connected from the session page":          testSessionConnectsToStream,
		"loads the SSE extension and glue":            testPageLoadsSSEExtension,
	} {
		t.Run(name, run)
	}
}

func testLogStreamsOneEventPerLine(t *testing.T) {
	t.Helper()
	t.Parallel()

	now := testNow
	logPath := filepath.Join(t.TempDir(), "run.jsonl")
	first, xss, third := jsonToolLine("first"), jsonToolLine(`<script>alert(1)</script>`), jsonToolLine("third")
	appendLines(t, logPath, first, xss, third)
	store, runID := runStore(t, logPath, now)
	endRun(t, store, runID, now)
	server := newServer(store, now)

	rec := do(t, server, http.MethodGet, logStreamPath(0), req{})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	if got := rec.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", got)
	}
	events := parseSSE(rec.Body)
	messages := messageEvents(events)
	if len(messages) != 3 {
		t.Fatalf("stream carried %d events, want 3: %q", len(messages), rec.Body)
	}
	if want := strconv.Itoa(len(first) + 1); messages[0].ID != want {
		t.Errorf("first event id = %q, want %q", messages[0].ID, want)
	}
	wantArgs := []string{"first", `<script>alert(1)</script>`, "third"}
	if diff := cmp.Diff(wantArgs, callArgs(t, events)); diff != "" {
		t.Errorf("streamed call args mismatch (-want +got):\n%s", diff)
	}
	for _, e := range messages {
		if scripts := htmltest.Parse(t, e.HTML()).Find("script"); len(scripts) != 0 {
			t.Errorf("stream carries %d live <script> elements, want 0", len(scripts))
		}
	}
}

func testLogStreamFilter(t *testing.T) {
	t.Helper()
	t.Parallel()

	logPath := writeRunLog(t, mustReadTestdata(t, "run.jsonl"))
	events := streamedFrom(t, logPath, "&log=fails")

	messages := messageEvents(events)
	if len(messages) != 1 {
		t.Fatalf("log=fails streamed %d events, want 1 (the run's one failure): %v", len(messages), messages)
	}
	if got := messages[0].HTML(); !strings.Contains(got, "FAIL") {
		t.Errorf("log=fails streamed %q, want the failing output", got)
	}
}

func testLogStreamMultiLineOutput(t *testing.T) {
	t.Helper()
	t.Parallel()

	logPath := writeRunLog(t, `{"type":"user","message":{"content":[{"type":"tool_result","is_error":true,`+
		`"content":"--- FAIL: TestX\nwant y"}]}}`+"\n")
	events := streamedFrom(t, logPath, "")

	messages := messageEvents(events)
	if len(messages) != 1 {
		t.Fatalf("stream carried %d events, want exactly 1: %v", len(messages), events)
	}
	failAt, wantAt := -1, -1
	for i, line := range messages[0].Data {
		switch {
		case strings.Contains(line, "--- FAIL: TestX"):
			failAt = i
		case strings.Contains(line, "want y"):
			wantAt = i
		}
	}
	if failAt < 0 || wantAt < 0 || failAt == wantAt {
		t.Errorf("output lines are on data lines %d and %d of %q, want one data line per output line",
			failAt, wantAt, messages[0].Data)
	}
}

func testLogStreamResumesFromOffset(t *testing.T) {
	t.Helper()
	t.Parallel()

	now := testNow
	logPath := filepath.Join(t.TempDir(), "run.jsonl")
	alreadyRead, newOne := jsonToolLine("already read"), jsonToolLine("new one")
	appendLines(t, logPath, alreadyRead, newOne)
	store, runID := runStore(t, logPath, now)
	endRun(t, store, runID, now)
	server := newServer(store, now)

	from := int64(len(alreadyRead) + 1)
	rec := do(t, server, http.MethodGet, logStreamPath(from), req{})

	body := rec.Body
	if strings.Contains(body, "already read") {
		t.Errorf("stream resent a line the fragment already rendered:\n%q", body)
	}
	if diff := cmp.Diff([]string{"new one"}, callArgs(t, parseSSE(body))); diff != "" {
		t.Errorf("streamed call args after the offset mismatch (-want +got):\n%s", diff)
	}
}

func testLogStreamEmptyWithoutRun(t *testing.T) {
	t.Helper()
	t.Parallel()

	now := testNow
	server := newServer(seededStore(t, now), now)

	rec := do(t, server, http.MethodGet, logStreamPath(0), req{})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	if body := rec.Body; body != "event: end\ndata:\n\n" {
		t.Errorf("stream for a ticket with no run is not an empty one that retires itself: %q", body)
	}
}

func testLogStreamErrorFrame(t *testing.T) {
	t.Helper()
	t.Parallel()

	now := testNow
	store, runID := runStore(t, t.TempDir(), now)
	endRun(t, store, runID, now)
	server := newServer(store, now)

	rec := do(t, server, http.MethodGet, logStreamPath(0), req{})

	if body := rec.Body; !strings.Contains(body, "event: error\ndata: ") {
		t.Errorf("stream = %q, want an error frame", body)
	}
}

func testLogStreamMalformedOffset(t *testing.T) {
	t.Helper()
	t.Parallel()

	now := testNow
	server := newServer(seededStore(t, now), now)

	rec := do(t, server, http.MethodGet, fmt.Sprintf("/ticket/%s/log?from=abc", url.PathEscape(logTicket)), req{})

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func testLogStreamRetiresWhenRunEnded(t *testing.T) {
	t.Helper()
	t.Parallel()

	now := testNow
	logPath := filepath.Join(t.TempDir(), "run.jsonl")
	appendLines(t, logPath, "last line")
	store, runID := runStore(t, logPath, now)
	endRun(t, store, runID, now)
	server := newServer(store, now)

	rec := do(t, server, http.MethodGet, logStreamPath(0), req{})

	if body := rec.Body; !strings.HasSuffix(body, "event: end\ndata:\n\n") {
		t.Errorf("the stream does not end on the sentinel sse-close listens for:\n%q", body)
	}
}

func testLogStreamResumesFromLastEventID(t *testing.T) {
	t.Helper()
	t.Parallel()

	now := testNow
	logPath := filepath.Join(t.TempDir(), "run.jsonl")
	swappedAlready, notYet := jsonToolLine("swapped already"), jsonToolLine("not yet")
	appendLines(t, logPath, swappedAlready, notYet)
	store, runID := runStore(t, logPath, now)
	endRun(t, store, runID, now)
	server := newServer(store, now)

	rec := do(t, server, http.MethodGet, logStreamPath(0), req{Header: http.Header{
		"Last-Event-Id": {strconv.Itoa(len(swappedAlready) + 1)},
	}})

	body := rec.Body
	if strings.Contains(body, "swapped already") {
		t.Errorf("a reconnect resent a line the browser had already swapped:\n%q", body)
	}
	if diff := cmp.Diff([]string{"not yet"}, callArgs(t, parseSSE(body))); diff != "" {
		t.Errorf("reconnect call args mismatch (-want +got):\n%s", diff)
	}
}

func testLogStreamFollowsUntilRunEnds(t *testing.T) {
	t.Helper()
	t.Parallel()

	now := testNow
	logPath := filepath.Join(t.TempDir(), "run.jsonl")
	appendLines(t, logPath, jsonToolLine("before"))
	store, runID := runStore(t, logPath, now)
	flushed := 0
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := w.(*httptest.ResponseRecorder)
		newServer(store, now).ServeHTTP(flushObserver{ResponseWriter: w, onFlush: func() {
			flushed++
			switch flushed {
			case 1:
				if body := rec.Body.String(); !strings.Contains(body, "before") || strings.Contains(body, "after") {
					t.Errorf("first flush = %q, want only the initial event", body)
				}
				appendLines(t, logPath, jsonToolLine("after"))
			case 2:
				if body := rec.Body.String(); !strings.Contains(body, "after") || strings.Contains(body, "event: end") {
					t.Errorf("second flush = %q, want the appended event before closure", body)
				}
				endRun(t, store, runID, now)
			}
		}}, r.WithContext(ctx))
	})
	got := do(t, h, http.MethodGet, logStreamPath(0), req{})
	if got.Code != http.StatusOK || !strings.Contains(got.Body, "before") || !strings.Contains(got.Body, "after") {
		t.Errorf("stream = %d %q, want both lines", got.Code, got.Body)
	}
	if first, second := strings.Index(got.Body, "before"), strings.Index(got.Body, "after"); first < 0 || second < first {
		t.Errorf("stream lines arrived out of order: %q", got.Body)
	}
	if !strings.Contains(got.Body, "event: end") {
		t.Errorf("stream did not close after run ended: %q", got.Body)
	}
}

type flushObserver struct {
	http.ResponseWriter
	onFlush func()
}

func (w flushObserver) Flush() {
	w.ResponseWriter.(http.Flusher).Flush()
	w.onFlush()
}

func testLogStreamLeavesRunAlone(t *testing.T) {
	t.Helper()
	t.Parallel()

	now := testNow
	logPath := filepath.Join(t.TempDir(), "run.jsonl")
	appendLines(t, logPath, jsonToolLine("still running"))
	store, _ := runStore(t, logPath, now)
	server := newServer(store, now)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	flushed := false
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		server.ServeHTTP(flushObserver{ResponseWriter: w, onFlush: func() {
			flushed = true
			if !strings.Contains(w.(*httptest.ResponseRecorder).Body.String(), "still running") {
				t.Error("client disconnected before the first stream event was flushed")
			}
			cancel()
		}}, r.WithContext(ctx))
	})
	do(t, h, http.MethodGet, logStreamPath(0), req{})
	if !flushed {
		t.Error("stream returned without a flush from which the client could disconnect")
	}

	runs, err := store.LatestRunsByTicket(t.Context())
	if err != nil {
		t.Fatalf("LatestRunsByTicket: %v", err)
	}
	run := runs[logTicket]
	if run.Outcome.IsSome() || run.EndedAt.IsSome() {
		t.Errorf("a disconnected reader disposed of the run: outcome=%v ended=%v", run.Outcome, run.EndedAt)
	}
	if run.Pgid != option.Some(4242) {
		t.Errorf("a disconnected reader changed the pgid: %v", run.Pgid)
	}
}

func testSessionConnectsToStream(t *testing.T) {
	t.Helper()
	t.Parallel()

	now := testNow
	logPath := writeLog(t, 3)
	info, err := os.Stat(logPath)
	if err != nil {
		t.Fatalf("Stat(%s): %v", logPath, err)
	}
	ticket := "https://github.com/o/r/issues/76"
	server := newServer(detailStore(t, logPath, now, now), now)

	rec := do(t, server, http.MethodGet, sessionPagePath(ticket), req{})
	main := htmltest.Parse(t, rec.Body).One(t, "main#main")
	stream := fmt.Sprintf("/ticket/%s/log?from=%d&records=0", url.PathEscape(ticket), info.Size())
	main.One(t, `[hx-ext="sse"][sse-connect="`+stream+`"][sse-swap="message"][sse-close="end"][hx-swap="beforeend"]`)
	for _, call := range main.Find(".call-head") {
		if call.One(t, ".call-arg").Text() == "step 3" {
			if got := call.One(t, ".call-verb").Text(); got != "Ran" {
				t.Errorf("call verb = %q, want Ran: %s", got, call.HTML())
			}
			return
		}
	}
	t.Error("session transcript has no step 3 call")
}

func testPageLoadsSSEExtension(t *testing.T) {
	t.Helper()
	t.Parallel()

	now := testNow
	server := newServer(seededStore(t, now), now)

	asset := do(t, server, http.MethodGet, "/assets/sse.min.js", req{})
	if asset.Code != http.StatusOK {
		t.Fatalf("GET /assets/sse.min.js = %d, want 200", asset.Code)
	}

	rec := do(t, server, http.MethodGet, "/tickets", req{})
	root := htmltest.Parse(t, rec.Body)
	root.One(t, `script[src="/assets/sse.min.js"]`)
	root.One(t, `script[type="module"][src="/assets/dist/glue.js"]`)
}
