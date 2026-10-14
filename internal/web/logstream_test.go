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

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/option"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
	"github.com/O-Marsters-1997/command-center/internal/web"
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
		Ticket: ticket.URL, Kind: "agent", BaselineSHA: "basesha1234", PromptHash: "hash-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordSpawn(t.Context(), storepkg.Spawn{
		RunID: plan.RunID(runID), Pgid: 4242, StartedAt: now, LogPath: logPath,
	}); err != nil {
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

	html, err := web.RenderLogLine(agentlog.Event{Kind: agentlog.Cmd, Tool: "Bash", Detail: text}, false)
	if err != nil {
		t.Fatal(err)
	}
	return string(html)
}

func endRun(t *testing.T, store *storepkg.Store, runID int64, at time.Time) {
	t.Helper()

	if err := store.RecordDisposition(t.Context(), storepkg.Disposition{
		RunID: plan.RunID(runID), Outcome: plan.OutcomePush, ExitCode: option.None[int](),
		Cost: option.None[float64](), EndedAt: at,
	}); err != nil {
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

	rec := do(t, server, http.MethodGet, logStreamPath(0), req{})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	if got := rec.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", got)
	}
	body := rec.Body
	firstOffset := int64(len(first) + 1)
	for _, want := range []string{
		fmt.Sprintf("id: %d\ndata: %s\n\n", firstOffset, renderedToolLine(t, "first")),
		`<code class="call-arg">&lt;script&gt;alert(1)&lt;/script&gt;</code>`,
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

	from := int64(len(alreadyRead) + 1)
	rec := do(t, server, http.MethodGet, logStreamPath(from), req{})

	body := rec.Body
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

	rec := do(t, server, http.MethodGet, logStreamPath(0), req{})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	if body := rec.Body; body != "event: end\ndata:\n\n" {
		t.Errorf("stream for a ticket with no run is not an empty one that retires itself: %q", body)
	}
}

func TestLogStreamSendsAnErrorFrameWhenTheLogCannotBeRead(t *testing.T) {
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

func TestLogStreamRefusesAMalformedOffset(t *testing.T) {
	t.Parallel()

	now := testNow
	server := newServer(seededStore(t, now), now)

	rec := do(t, server, http.MethodGet, fmt.Sprintf("/ticket/%s/log?from=abc", url.PathEscape(logTicket)), req{})

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
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

	rec := do(t, server, http.MethodGet, logStreamPath(0), req{})

	if body := rec.Body; !strings.HasSuffix(body, "event: end\ndata:\n\n") {
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

	rec := do(t, server, http.MethodGet, logStreamPath(0), req{Header: http.Header{
		"Last-Event-Id": {strconv.Itoa(len(swappedAlready) + 1)},
	}})

	body := rec.Body
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

func TestLogStreamLeavesTheRunAloneWhenTheClientGoesAway(t *testing.T) {
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
		t.Fatal(err)
	}
	run := runs[logTicket]
	if run.Outcome.IsSome() || run.EndedAt.IsSome() {
		t.Errorf("a disconnected reader disposed of the run: outcome=%v ended=%v", run.Outcome, run.EndedAt)
	}
	if run.Pgid != option.Some(4242) {
		t.Errorf("a disconnected reader changed the pgid: %v", run.Pgid)
	}
}

func TestSessionConnectsThePreToTheStream(t *testing.T) {
	t.Parallel()

	now := testNow
	logPath := writeLog(t, 3)
	info, err := os.Stat(logPath)
	if err != nil {
		t.Fatal(err)
	}
	ticket := "https://github.com/o/r/issues/76"
	server := newServer(detailStore(t, logPath, now, now), now)

	rec := do(t, server, http.MethodGet, sessionPagePath(ticket), req{})
	body := rec.Body

	stream := fmt.Sprintf("/ticket/%s/log?from=%d&amp;records=0", url.PathEscape(ticket), info.Size())
	for _, want := range []string{
		`hx-ext="sse"`,
		`sse-connect="` + stream + `"`,
		`sse-swap="message"`,
		`sse-close="end"`,
		`hx-swap="beforeend"`,
		`<span class="call-verb">Ran</span><code class="call-arg">step 3</code>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("session page is missing %q:\n%s", want, body)
		}
	}
}

func TestPageLoadsTheSSEExtensionAndGlue(t *testing.T) {
	t.Parallel()

	now := testNow
	server := newServer(seededStore(t, now), now)

	asset := do(t, server, http.MethodGet, "/assets/sse.min.js", req{})
	if asset.Code != http.StatusOK {
		t.Fatalf("GET /assets/sse.min.js = %d, want 200", asset.Code)
	}

	rec := do(t, server, http.MethodGet, "/tickets", req{})
	body := rec.Body
	for _, want := range []string{
		`<script src="/assets/sse.min.js"></script>`,
		`<script type="module" src="/assets/dist/glue.js"></script>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page is missing %q:\n%s", want, body)
		}
	}
}
