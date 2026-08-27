package cc_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
	"github.com/O-Marsters-1997/command-center/internal/gh"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

// detailStore seeds one running task with a worktree, a live PR carrying two checks and a log
// file on disk: every value issue #76 asks the fragment to carry, in one fixture.
func detailStore(t *testing.T, logPath string, startedAt, now time.Time) *cc.Store {
	t.Helper()

	ctx := t.Context()
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	task := cc.Task{TicketURL: "https://github.com/o/r/issues/76", Repo: "repo", Branch: "cc-76"}
	if err := store.UpsertTasks(ctx, []cc.Task{task}); err != nil {
		t.Fatal(err)
	}
	runID, err := store.InsertRunSkeleton(ctx, task.TicketURL, "agent", "basesha1234", "hash-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordSpawn(ctx, runID, 4242, startedAt, logPath); err != nil {
		t.Fatal(err)
	}
	obs := cc.Observation{
		ObservedAt: now,
		Runs:       map[string]cc.RunObservation{task.TicketURL: {Alive: true}},
		Worktrees:  map[string]string{"cc-76": "/repos/repo-cc-76"},
		PRs: map[string]gh.PR{"cc-76": {
			Number: 76, State: gh.Open, HeadRef: "cc-76",
			Checks: map[string]gh.CheckState{
				"unit":  {Status: "COMPLETED", Conclusion: "SUCCESS"},
				"build": {Status: "IN_PROGRESS"},
			},
		}},
	}
	if err := store.SaveObservation(ctx, obs); err != nil {
		t.Fatal(err)
	}
	return store
}

func writeLog(t *testing.T, lines int) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "run.jsonl")
	var b strings.Builder
	for i := 1; i <= lines; i++ {
		fmt.Fprintf(&b, "line xxx %d\n", i)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func detailPath(ticketURL string) string {
	return "/task/" + url.PathEscape(ticketURL) + "/detail"
}

// TestDetailFragmentCarriesEveryRowFact covers issue #76 AC2: one fragment, log tail, checks,
// base SHA, elapsed and worktree.
func TestDetailFragmentCarriesEveryRowFact(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	now := startedAt.Add(90 * time.Second)
	logPath := writeLog(t, 120)
	server := cc.NewServer(detailStore(t, logPath, startedAt, now), fixedClock(now), nil, nil, "")

	rec := httptest.NewRecorder()
	target := detailPath("https://github.com/o/r/issues/76")
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200: %s", target, rec.Code, rec.Body)
	}

	body := rec.Body.String()
	for _, want := range []string{
		"basesha1234",
		"/repos/repo-cc-76",
		"1m30s",
		"unit",
		"SUCCESS",
		"build",
		"IN_PROGRESS",
		logPath,
		"line xxx 120",
		"line xxx 71",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("detail fragment is missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "line xxx 70\n") {
		t.Errorf("detail fragment tails more than fifty lines:\n%s", body)
	}
}

// TestDetailRejectsAnUnknownTask covers issue #76 AC3's second half.
func TestDetailRejectsAnUnknownTask(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	server := cc.NewServer(seededStore(t, now), fixedClock(now), nil, nil, "")

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, detailPath("sandbox://NOPE"), nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404: %s", rec.Code, rec.Body)
	}
}

// TestDetailIsTheSameDerivationAsTheBoardRow covers issue #76 AC5: the fragment reuses derive's
// output, so every value it prints is the value the board printed for the same row.
func TestDetailIsTheSameDerivationAsTheBoardRow(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	now := startedAt.Add(90 * time.Second)
	server := cc.NewServer(detailStore(t, writeLog(t, 3), startedAt, now), fixedClock(now), nil, nil, "")

	board := httptest.NewRecorder()
	server.ServeHTTP(board, httptest.NewRequest(http.MethodGet, "/", nil))
	detail := httptest.NewRecorder()
	server.ServeHTTP(detail, httptest.NewRequest(
		http.MethodGet, detailPath("https://github.com/o/r/issues/76"), nil))

	for _, shared := range []string{"/repos/repo-cc-76", "1m30s"} {
		if !strings.Contains(board.Body.String(), shared) {
			t.Fatalf("board is missing %q, so the fragment cannot be compared against it", shared)
		}
		if !strings.Contains(detail.Body.String(), shared) {
			t.Errorf("detail fragment does not carry the board's own %q", shared)
		}
	}
}

// TestBoardLinksEveryRowToItsOwnDetail covers issue #76 AC3's first half: the percent-encoded
// ticket URL the board emits is the one that resolves.
func TestBoardLinksEveryRowToItsOwnDetail(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	server := cc.NewServer(detailStore(t, writeLog(t, 1), now, now), fixedClock(now), nil, nil, "")

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()

	want := `hx-get="` + detailPath("https://github.com/o/r/issues/76") + `"`
	if !strings.Contains(body, want) {
		t.Fatalf("board is missing %s:\n%s", want, body)
	}
}

// TestHTMXIsServedFromTheBinary covers issue #76 AC1: vendored and local, so the page loads with
// no network.
func TestHTMXIsServedFromTheBinary(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	server := cc.NewServer(seededStore(t, now), fixedClock(now), nil, nil, "")

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/htmx.min.js", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /assets/htmx.min.js = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "htmx") {
		t.Error("/assets/htmx.min.js does not serve htmx")
	}

	page := httptest.NewRecorder()
	server.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/", nil))
	body := page.Body.String()
	if !strings.Contains(body, `<script src="/assets/htmx.min.js"></script>`) {
		t.Errorf("page does not load htmx from the binary:\n%s", body)
	}
	if strings.Contains(body, "//unpkg.com") || strings.Contains(body, "//cdn.") {
		t.Error("page loads htmx over the network")
	}
}

// TestBoardPollsItselfInsteadOfReloading covers issue #76 AC4.
func TestBoardPollsItselfInsteadOfReloading(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	server := cc.NewServer(seededStore(t, now), fixedClock(now), nil, nil, "")

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()

	if strings.Contains(body, "http-equiv=\"refresh\"") {
		t.Error("the meta refresh is still on the page")
	}
	for _, want := range []string{
		`<div id="board" hx-get="/board" hx-trigger="every 5s">`,
		`<div id="liveness" hx-swap-oob="true">`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page is missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "hx-select") {
		t.Errorf("the poll re-renders the whole document to select the board back out of it:\n%s", body)
	}
}

// TestDetailRowsSurviveTheBoardSwap covers the PRD's standing constraint: every swap target's
// island container carries hx-preserve and a stable id.
func TestDetailRowsSurviveTheBoardSwap(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	server := cc.NewServer(detailStore(t, writeLog(t, 1), now, now), fixedClock(now), nil, nil, "")

	first := httptest.NewRecorder()
	server.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/", nil))
	second := httptest.NewRecorder()
	server.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/", nil))

	id := detailRowID(t, first.Body.String())
	if !strings.Contains(second.Body.String(), id) {
		t.Errorf("the detail row id %q is not stable across renders", id)
	}
	if !strings.Contains(first.Body.String(), id+`" hx-preserve="true"`) {
		t.Errorf("the detail row does not carry hx-preserve:\n%s", first.Body.String())
	}
}

func detailRowID(t *testing.T, body string) string {
	t.Helper()

	const marker = `<tr id="detail-`
	i := strings.Index(body, marker)
	if i < 0 {
		t.Fatalf("no detail row in:\n%s", body)
	}
	rest := body[i+len(`<tr id="`):]
	return `id="` + rest[:strings.Index(rest, `"`)]
}

// TestTailLogReadsBackPastItsWindow covers the branch a small log never reaches. An agent log
// line is a whole JSON event, so a file can run past the window with fewer than fifty lines in
// it: the tail must then be every whole line the window holds, clipping only the one it cut.
func TestTailLogReadsBackPastItsWindow(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		lineLength int
	}{
		// 65536 is a whole number of 2048-byte lines, so the window opens exactly on a line
		// break -- where dropping "the clipped first line" drops a whole one instead.
		{"window opens on a break", 2048},
		{"window opens mid-line", 2000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			const lines = 60
			path := filepath.Join(t.TempDir(), "run.jsonl")
			var b strings.Builder
			for i := 1; i <= lines; i++ {
				line := fmt.Sprintf("line %04d ", i)
				b.WriteString(line + strings.Repeat("x", tc.lineLength-len(line)-1) + "\n")
			}
			if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
				t.Fatal(err)
			}

			got, _ := cc.TailLog(path)
			for i, line := range got {
				if len(line) != tc.lineLength-1 {
					t.Fatalf("line %d is clipped at %d bytes: %.20q...", i, len(line), line)
				}
			}
			if last := got[len(got)-1]; !strings.HasPrefix(last, "line 0060 ") {
				t.Errorf("tail does not end at the log's own last line: %.20q...", last)
			}
			// 64 KiB holds this many whole lines; one fewer is the off-by-one at the boundary.
			if want := 64 << 10 / tc.lineLength; len(got) != want {
				t.Errorf("tail has %d lines, want %d: the window dropped a whole line", len(got), want)
			}
		})
	}
}

// TestDetailSpansEveryBoardColumn keeps the fragment's colspan honest against the board's own
// header, which lives in the other template and nothing else compares them.
func TestDetailSpansEveryBoardColumn(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	server := cc.NewServer(detailStore(t, writeLog(t, 1), now, now), fixedClock(now), nil, nil, "")

	board := httptest.NewRecorder()
	server.ServeHTTP(board, httptest.NewRequest(http.MethodGet, "/", nil))
	columns := strings.Count(board.Body.String(), "<th>")

	detail := httptest.NewRecorder()
	server.ServeHTTP(detail, httptest.NewRequest(
		http.MethodGet, detailPath("https://github.com/o/r/issues/76"), nil))

	want := fmt.Sprintf(`colspan="%d"`, columns)
	if !strings.Contains(detail.Body.String(), want) {
		t.Errorf("the board has %d columns; the detail fragment does not carry %s", columns, want)
	}
}

// hxAttrRE finds every htmx attribute in the rendered board, with the tag it sits on.
var hxAttrRE = regexp.MustCompile(`<(\w+)([^>]*\shx-[\w:-]+=[^>]*)>`)

// TestVerbsNeedNoJavaScript covers issue #76 AC5's second half. htmx is liveness and row detail
// only: it never becomes load-bearing for a verb, so the board still works with scripting off.
func TestVerbsNeedNoJavaScript(t *testing.T) {
	t.Parallel()

	states := make([]plan.State, 0, int(plan.RefreshConflicted)+1)
	for s := plan.Blocked; s <= plan.RefreshConflicted; s++ {
		states = append(states, s)
	}
	board, err := cc.RenderStatesPage(states)
	if err != nil {
		t.Fatal(err)
	}

	for _, m := range hxAttrRE.FindAllStringSubmatch(board, -1) {
		tag, attrs := m[1], m[2]
		switch {
		case tag == "table", tag == "div", tag == "tr":
		case tag == "button" && strings.Contains(attrs, "/detail"):
		default:
			t.Errorf("a verb control carries htmx and so needs JavaScript: <%s%s>", tag, attrs)
		}
	}

	// Every verb still reaches the server the way it did before htmx: a form the browser submits.
	for _, want := range []string{
		`<form method="post" action="/verb">`,
		`<form method="get" action="/confirm">`,
		`<form id="launch" method="get" action="/preview"></form>`,
		`<input type="checkbox" form="launch" name="task"`,
	} {
		if !strings.Contains(board, want) {
			t.Errorf("the board is missing the scriptless path %q", want)
		}
	}
	if strings.Contains(board, "hx-post") {
		t.Error("a verb posts over htmx rather than a form")
	}
}
