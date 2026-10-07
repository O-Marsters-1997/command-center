package web_test

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
	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web"
)

const logTicket = "https://github.com/o/r/issues/77"

func logStreamPath(from int64) string {
	return fmt.Sprintf("/ticket/%s/log?from=%d", url.PathEscape(logTicket), from)
}

func runStore(t *testing.T, logPath string, now time.Time) (*storepkg.Store, int64) {
	t.Helper()

	ctx := t.Context()
	store := openStore(t)
	ticket := storepkg.Ticket{URL: logTicket, Repo: "repo", Branch: "cc-77"}
	if err := store.UpsertTickets(ctx, []storepkg.Ticket{ticket}); err != nil {
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

func jsonToolLine(text string) string {
	return fmt.Sprintf(`{"type":"assistant","timestamp":"2026-08-20T12:00:00Z",`+
		`"message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":%q}}]}}`, text)
}

func renderedToolLine(t *testing.T, text string) string {
	t.Helper()

	html, err := web.RenderLogLine(agentlog.Event{Kind: agentlog.Tool, Tool: "Bash", Detail: text}, false)
	if err != nil {
		t.Fatal(err)
	}
	return string(html)
}

func endRun(t *testing.T, store *storepkg.Store, runID int64, at time.Time) {
	t.Helper()

	if err := store.RecordDisposition(t.Context(), runID, plan.OutcomePush, nil, at, nil); err != nil {
		t.Fatal(err)
	}
}

func TestLogStreamsOneEventPerLine(t *testing.T) {
	t.Parallel()

	now := testNow
	logPath := filepath.Join(t.TempDir(), "run.jsonl")
	first, xss, third := jsonToolLine("first"), jsonToolLine(`<script>alert(1)</script>`), jsonToolLine("third")
	appendLines(t, logPath, first, xss, third)
	store, runID := runStore(t, logPath, now)
	endRun(t, store, runID, now)
	server := newServer(store, now)

	rec := get(t, server, logStreamPath(0))

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
		`data: <div class="line line-tool"><span class="line-label">tool</span> Bash ` +
			"&lt;script&gt;alert(1)&lt;/script&gt;</div>\n\n",
		"data: " + renderedToolLine(t, "third") + "\n\n",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("stream is missing %q:\n%q", want, body)
		}
	}
}

func TestLogStreamResumesFromTheOffsetTheFragmentRendered(t *testing.T) {
	t.Parallel()

	now := testNow
	logPath := filepath.Join(t.TempDir(), "run.jsonl")
	alreadyRead, newOne := jsonToolLine("already read"), jsonToolLine("new one")
	appendLines(t, logPath, alreadyRead, newOne)
	store, runID := runStore(t, logPath, now)
	endRun(t, store, runID, now)
	server := newServer(store, now)

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

func TestLogStreamIsEmptyForATicketWithNoRun(t *testing.T) {
	t.Parallel()

	now := testNow
	server := newServer(seededStore(t, now), now)

	rec := get(t, server, logStreamPath(0))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	if body := rec.Body.String(); body != "event: end\ndata:\n\n" {
		t.Errorf("stream for a ticket with no run is not an empty one that retires itself: %q", body)
	}
}

func TestLogStreamRetiresItselfWhenTheRunHasEnded(t *testing.T) {
	t.Parallel()

	now := testNow
	logPath := filepath.Join(t.TempDir(), "run.jsonl")
	appendLines(t, logPath, "last line")
	store, runID := runStore(t, logPath, now)
	endRun(t, store, runID, now)
	server := newServer(store, now)

	rec := get(t, server, logStreamPath(0))

	if body := rec.Body.String(); !strings.HasSuffix(body, "event: end\ndata:\n\n") {
		t.Errorf("the stream does not end on the sentinel sse-close listens for:\n%q", body)
	}
}

func TestLogStreamResumesAReconnectFromItsLastEventID(t *testing.T) {
	t.Parallel()

	now := testNow
	logPath := filepath.Join(t.TempDir(), "run.jsonl")
	swappedAlready, notYet := jsonToolLine("swapped already"), jsonToolLine("not yet")
	appendLines(t, logPath, swappedAlready, notYet)
	store, runID := runStore(t, logPath, now)
	endRun(t, store, runID, now)
	server := newServer(store, now)

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

func TestLogStreamFollowsUntilTheRunEnds(t *testing.T) {
	t.Parallel()

	now := testNow
	logPath := filepath.Join(t.TempDir(), "run.jsonl")
	appendLines(t, logPath, jsonToolLine("before"))
	store, runID := runStore(t, logPath, now)
	httpServer := httptest.NewServer(newServer(store, now))
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

func TestLogStreamLeavesTheRunAloneWhenTheClientGoesAway(t *testing.T) {
	t.Parallel()

	now := testNow
	logPath := filepath.Join(t.TempDir(), "run.jsonl")
	appendLines(t, logPath, "still running")
	store, _ := runStore(t, logPath, now)
	server := newServer(store, now)

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

func TestDetailConnectsThePreToTheStream(t *testing.T) {
	t.Parallel()

	now := testNow
	logPath := writeLog(t, 3)
	info, err := os.Stat(logPath)
	if err != nil {
		t.Fatal(err)
	}
	ticket := "https://github.com/o/r/issues/76"
	server := newServer(detailStore(t, logPath, now, now), now)

	rec := get(t, server, selPagePath(ticket))
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

func TestPageCapsThePreAtAThousandLines(t *testing.T) {
	t.Parallel()

	now := testNow
	server := newServer(seededStore(t, now), now)

	asset := get(t, server, "/assets/sse.min.js")
	if asset.Code != http.StatusOK {
		t.Fatalf("GET /assets/sse.min.js = %d, want 200", asset.Code)
	}

	rec := get(t, server, "/")
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
