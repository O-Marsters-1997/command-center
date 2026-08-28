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

// detailStore seeds one running ticket with a worktree, a live PR carrying two checks and a log
// file on disk: every value issue #76 asks the fragment to carry, in one fixture.
func detailStore(t *testing.T, logPath string, startedAt, now time.Time) *cc.Store {
	t.Helper()

	ctx := t.Context()
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	ticket := cc.Ticket{URL: "https://github.com/o/r/issues/76", Repo: "repo", Branch: "cc-76"}
	if err := store.UpsertTickets(ctx, []cc.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}
	runID, err := store.InsertRunSkeleton(ctx, ticket.URL, "agent", "basesha1234", "hash-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordSpawn(ctx, runID, 4242, startedAt, logPath); err != nil {
		t.Fatal(err)
	}
	obs := cc.Observation{
		ObservedAt: now,
		Runs:       map[string]cc.RunObservation{ticket.URL: {Alive: true}},
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

func selPagePath(ticketURL string) string {
	return "/?" + url.Values{"sel": {ticketURL}}.Encode()
}

// TestDetailFragmentCarriesEveryRowFact covers issue #76 AC2: one fragment, log tail, checks,
// base SHA, elapsed and worktree.
func TestDetailFragmentCarriesEveryRowFact(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	now := startedAt.Add(90 * time.Second)
	logPath := writeLog(t, 120)
	server := cc.NewServer(detailStore(t, logPath, startedAt, now), fixedClock(now), nil, "")

	rec := httptest.NewRecorder()
	target := selPagePath("https://github.com/o/r/issues/76")
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

func TestSelectingAnUnknownTicketRendersNothingSelected(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	server := cc.NewServer(seededStore(t, now), fixedClock(now), nil, "")

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, selPagePath("sandbox://NOPE"), nil))
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "hx-preserve") {
		t.Errorf("an unmatched ?sel= still rendered a detail row:\n%s", rec.Body)
	}
}

func TestTheDeletedDetailRouteIs404(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	server := cc.NewServer(seededStore(t, now), fixedClock(now), nil, "")

	// The issue's own route spelling and the pre-existing code's both 404, since either could
	// still be bookmarked or linked.
	for _, target := range []string{
		"/ticket/" + url.PathEscape("sandbox://CC-1") + "/detail",
		"/task/" + url.PathEscape("sandbox://CC-1") + "/detail",
	} {
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", target, rec.Code)
		}
	}
}

func TestDetailIsTheSameDerivationAsTheBoardRow(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	now := startedAt.Add(90 * time.Second)
	server := cc.NewServer(detailStore(t, writeLog(t, 3), startedAt, now), fixedClock(now), nil, "")

	board := httptest.NewRecorder()
	server.ServeHTTP(board, httptest.NewRequest(http.MethodGet, "/", nil))
	selected := httptest.NewRecorder()
	server.ServeHTTP(selected, httptest.NewRequest(
		http.MethodGet, selPagePath("https://github.com/o/r/issues/76"), nil))

	for _, shared := range []string{"/repos/repo-cc-76", "1m30s"} {
		if !strings.Contains(board.Body.String(), shared) {
			t.Fatalf("board is missing %q, so the fragment cannot be compared against it", shared)
		}
		if !strings.Contains(selected.Body.String(), shared) {
			t.Errorf("selected render does not carry the board's own %q", shared)
		}
	}
}

func TestBoardLinksEveryRowToItsOwnSelection(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	server := cc.NewServer(detailStore(t, writeLog(t, 1), now, now), fixedClock(now), nil, "")

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()

	want := `hx-get="/board?` + url.Values{"sel": {"https://github.com/o/r/issues/76"}}.Encode() + `"`
	if !strings.Contains(body, want) {
		t.Fatalf("board is missing %s:\n%s", want, body)
	}
}

// TestHTMXIsServedFromTheBinary covers issue #76 AC1: vendored and local, so the page loads with
// no network.
func TestHTMXIsServedFromTheBinary(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	server := cc.NewServer(seededStore(t, now), fixedClock(now), nil, "")

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

func TestBoardPollsItselfInsteadOfReloading(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	server := cc.NewServer(seededStore(t, now), fixedClock(now), nil, "")

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()

	if strings.Contains(body, "http-equiv=\"refresh\"") {
		t.Error("the meta refresh is still on the page")
	}
	for _, want := range []string{
		`<table id="board" hx-get="/board" hx-trigger="every 5s" hx-swap="outerHTML">`,
		`<div id="masthead" hx-swap-oob="true">`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page is missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "hx-select") {
		t.Errorf("the poll re-renders the whole document to select the board back out of it:\n%s", body)
	}
}

// tc.from/tc.to model a (previously selected, now selected) pair across a board swap.
func TestOnlyTheSelectedRowCarriesADetailRow(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	tickets := []cc.Ticket{
		{URL: "sandbox://A", Repo: "repo", Branch: "a"},
		{URL: "sandbox://B", Repo: "repo", Branch: "b"},
		{URL: "sandbox://C", Repo: "repo", Branch: "c"},
	}
	if err := store.UpsertTickets(ctx, tickets); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	if err := store.SaveObservation(ctx, cc.Observation{ObservedAt: now}); err != nil {
		t.Fatal(err)
	}
	server := cc.NewServer(store, fixedClock(now), []cc.Repo{{Name: "repo"}}, "")
	all := []string{"sandbox://A", "sandbox://B", "sandbox://C"}

	for _, tc := range []struct{ from, to string }{
		{"", ""},
		{"", "sandbox://A"},
		{"sandbox://A", "sandbox://B"},
		{"sandbox://A", "sandbox://C"},
	} {
		t.Run(fmt.Sprintf("%s to %s", tc.from, tc.to), func(t *testing.T) {
			t.Parallel()

			target := "/"
			if tc.to != "" {
				target = selPagePath(tc.to)
			}
			rec := httptest.NewRecorder()
			server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
			body := rec.Body.String()

			for _, url := range all {
				id := detailIDFor(server, url)
				if url == tc.to {
					if !strings.Contains(body, id+`" hx-preserve="true"`) {
						t.Errorf("no preserved detail row for the now-selected %s:\n%s", url, body)
					}
					continue
				}
				if strings.Contains(body, id) {
					t.Errorf("%s still carries a detail row after selecting %s:\n%s", url, tc.to, body)
				}
			}
			wantCount := 0
			if tc.to != "" {
				wantCount = 1
			}
			if got := strings.Count(body, `hx-preserve="true"`); got != wantCount {
				t.Errorf("%d preserved rows, want %d:\n%s", got, wantCount, body)
			}
		})
	}
}

func detailIDFor(server *cc.Server, ticketURL string) string {
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, selPagePath(ticketURL), nil))
	m := regexp.MustCompile(`<tr id="(detail-[0-9a-f]+)" hx-preserve="true">`).FindStringSubmatch(rec.Body.String())
	return m[1]
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

func TestDetailRowsSurviveTheBoardSwap(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	server := cc.NewServer(detailStore(t, writeLog(t, 1), now, now), fixedClock(now), nil, "")
	target := selPagePath("https://github.com/o/r/issues/76")

	var id, stream string
	for i := range 3 {
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		body := rec.Body.String()

		gotID := detailRowID(t, body)
		if !strings.Contains(body, gotID+`" hx-preserve="true"`) {
			t.Fatalf("swap %d: the detail row does not carry hx-preserve:\n%s", i, body)
		}
		gotStream := sseConnectStream(t, body)
		if i == 0 {
			id, stream = gotID, gotStream
			continue
		}
		if gotID != id {
			t.Errorf("swap %d: detail row id changed from %q to %q", i, id, gotID)
		}
		if gotStream != stream {
			t.Errorf("swap %d: SSE resume offset changed from %q to %q", i, stream, gotStream)
		}
	}
}

func sseConnectStream(t *testing.T, body string) string {
	t.Helper()

	m := regexp.MustCompile(`sse-connect="([^"]*)"`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no sse-connect in:\n%s", body)
	}
	return m[1]
}

func TestSelectingASecondRowRemovesTheFirstsDetail(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	tickets := []cc.Ticket{
		{URL: "sandbox://A", Repo: "repo", Branch: "a"},
		{URL: "sandbox://B", Repo: "repo", Branch: "b"},
	}
	if err := store.UpsertTickets(ctx, tickets); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	if err := store.SaveObservation(ctx, cc.Observation{ObservedAt: now}); err != nil {
		t.Fatal(err)
	}
	server := cc.NewServer(store, fixedClock(now), []cc.Repo{{Name: "repo"}}, "")

	first := httptest.NewRecorder()
	server.ServeHTTP(first, httptest.NewRequest(http.MethodGet, selPagePath("sandbox://A"), nil))
	firstID := detailRowID(t, first.Body.String())

	second := httptest.NewRecorder()
	server.ServeHTTP(second, httptest.NewRequest(http.MethodGet, selPagePath("sandbox://B"), nil))
	body := second.Body.String()

	if strings.Contains(body, firstID) {
		t.Errorf("selecting sandbox://B still carries sandbox://A's detail row %q:\n%s", firstID, body)
	}
	if strings.Count(body, `hx-preserve="true"`) != 1 {
		t.Errorf("board carries more than one detail row:\n%s", body)
	}
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
	server := cc.NewServer(detailStore(t, writeLog(t, 1), now, now), fixedClock(now), nil, "")

	board := httptest.NewRecorder()
	server.ServeHTTP(board, httptest.NewRequest(http.MethodGet, "/", nil))
	columns := strings.Count(board.Body.String(), "<th>")

	selected := httptest.NewRecorder()
	server.ServeHTTP(selected, httptest.NewRequest(
		http.MethodGet, selPagePath("https://github.com/o/r/issues/76"), nil))

	want := fmt.Sprintf(`colspan="%d"`, columns)
	if !strings.Contains(selected.Body.String(), want) {
		t.Errorf("the board has %d columns; the detail fragment does not carry %s", columns, want)
	}
}

// hxAttrRE finds every htmx attribute in the rendered board, with the tag it sits on.
var hxAttrRE = regexp.MustCompile(`<(\w+)([^>]*\shx-[\w:-]+=[^>]*)>`)

// The launch checkbox and the row's own select control both carry hx-target="#board" as a
// progressive enhancement over a plain checkbox and a plain (no-op without JS) button — neither
// needs it to do its real job, so both are allowed here alongside the structural containers.
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
		if tag == "table" || tag == "div" || tag == "tr" {
			continue
		}
		if (tag == "input" || tag == "button") && strings.Contains(attrs, `hx-target="#board"`) {
			continue
		}
		t.Errorf("a verb control carries htmx and so needs JavaScript: <%s%s>", tag, attrs)
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
