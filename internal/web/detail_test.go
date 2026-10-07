package web_test

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

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web"
)

func detailStore(t *testing.T, logPath string, startedAt, now time.Time) *storepkg.Store {
	t.Helper()

	ctx := t.Context()
	store := openStore(t)
	ticket := storepkg.Ticket{URL: "https://github.com/o/r/issues/76", Repo: "repo", Branch: "cc-76"}
	if err := store.UpsertTickets(ctx, []storepkg.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}
	runID, err := store.InsertRunSkeleton(ctx, ticket.URL, "agent", "basesha1234", "hash-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordSpawn(ctx, runID, 4242, startedAt, logPath); err != nil {
		t.Fatal(err)
	}
	obs := plan.Observation{
		ObservedAt: now,
		Runs:       map[string]plan.RunObservation{ticket.URL: {Alive: true}},
		Worktrees:  map[string]string{plan.BranchKey("repo", "cc-76"): "/repos/repo-cc-76"},
		PRs: map[string]plan.PR{plan.BranchKey("repo", "cc-76"): {
			Number: 76, State: plan.Open, HeadRef: "cc-76",
			Checks: map[string]plan.CheckState{
				"unit":  {Status: "COMPLETED", Conclusion: "SUCCESS"},
				"build": {Status: "IN_PROGRESS", DetailsURL: "https://github.com/o/r/actions/runs/1"},
			},
		}},
	}
	if err := store.SaveObservation(ctx, obs); err != nil {
		t.Fatal(err)
	}
	return store
}

func writeLog(t *testing.T, n int) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "run.jsonl")
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, `{"type":"assistant","timestamp":"2026-08-20T12:00:%02dZ",`+
			`"message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"step %d"}}]}}`+"\n",
			i%60, i)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func selPagePath(ticketURL string) string {
	return "/?" + url.Values{"sel": {ticketURL}}.Encode()
}

func TestDetailFragmentCarriesEveryRowFact(t *testing.T) {
	t.Parallel()

	startedAt := testNow
	now := startedAt.Add(90 * time.Second)
	logPath := writeLog(t, 120)
	server := newServer(detailStore(t, logPath, startedAt, now), now)

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
		"120 lines",
		"step 1",
		"step 120",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("detail fragment is missing %q:\n%s", want, body)
		}
	}

	linked := `<a href="https://github.com/o/r/actions/runs/1" target="_blank" rel="noopener">build</a>`
	if !strings.Contains(body, linked) {
		t.Errorf("build's check name is not linked to its DetailsURL:\n%s", body)
	}
	if strings.Contains(body, `<a href=""`) {
		t.Errorf("a check with no DetailsURL rendered an empty anchor:\n%s", body)
	}
	if !strings.Contains(body, "<div>unit:") {
		t.Errorf("unit has no DetailsURL and should render as plain text:\n%s", body)
	}
}

func TestDetailShowsTheContextCurveOnlyForADisposedRunWithRows(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	now := testNow

	withRows := detailStore(t, writeLog(t, 1), now, now)
	runID, err := withRows.InsertRunSkeleton(ctx, "https://github.com/o/r/issues/76", "agent", "basesha1234", "hash-1")
	if err != nil {
		t.Fatal(err)
	}
	metrics := agentlog.RunMetrics{
		TokensIn: 100, TokensOut: 12, Settled: true,
		Requests: []agentlog.Request{
			{ID: "r1", Thread: agentlog.MainThread,
				InputTokens: 10, CacheCreationTokens: 20, CacheReadTokens: 30, OutputTokens: 5},
			{ID: "r2", Thread: agentlog.MainThread,
				InputTokens: 40, CacheCreationTokens: 50, CacheReadTokens: 60, OutputTokens: 7},
		},
	}
	if err := withRows.RecordDisposition(ctx, runID, plan.OutcomePush, nil, now, &metrics); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	server := newServer(withRows, now)
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, selPagePath("https://github.com/o/r/issues/76"), nil))
	body := rec.Body.String()
	if !strings.Contains(body, `class="context-curve`) {
		t.Errorf("no context curve for a disposed run with run_requests rows:\n%s", body)
	}
	if !strings.Contains(body, `chart-series-0`) {
		t.Errorf("no main-thread series in the curve:\n%s", body)
	}

	noRows := detailStore(t, writeLog(t, 1), now, now)
	rec2 := httptest.NewRecorder()
	server2 := newServer(noRows, now)
	server2.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, selPagePath("https://github.com/o/r/issues/76"), nil))
	if strings.Contains(rec2.Body.String(), `class="context-curve`) {
		t.Errorf("a run with no run_requests rows still rendered a context curve:\n%s", rec2.Body)
	}
}

func TestDetailFragmentOffersFollowUpOnlyInTheDetailNotTheRow(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTickets(ctx, []storepkg.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}

	at := testNow
	runID, err := store.InsertRunSkeleton(ctx, ticket.URL, "agent", "basesha1234", "hash-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordSpawn(ctx, runID, 111, at, "/state/runs/1.jsonl"); err != nil {
		t.Fatal(err)
	}
	exitCode := 1
	if err := store.RecordDisposition(ctx, runID, plan.OutcomeFailed, &exitCode, at, nil); err != nil {
		t.Fatal(err)
	}
	obs := plan.Observation{
		ObservedAt: at,
		Worktrees:  map[string]string{plan.BranchKey("repo", "cc-1"): "/repos/repo-cc-1"},
		PRs:        map[string]plan.PR{},
	}
	if err := store.SaveObservation(ctx, obs); err != nil {
		t.Fatal(err)
	}

	server := web.NewServer(store, fixedClock(at), []config.Repo{{Name: "repo"}}, "")
	page := renderPage(t, server)
	if state := rowState(t, page, ticket.URL); state != "failed" {
		t.Fatalf("state = %q, want failed", state)
	}

	row := rowHTML(t, page, ticket.URL)
	if strings.Contains(row, `value="follow-up"`) {
		t.Errorf("the row itself must never offer a follow-up button, want it only in the detail:\n%s", row)
	}

	selected := renderPath(t, server, selPagePath(ticket.URL))
	if !strings.Contains(selected, `<textarea name="prompt"`) {
		t.Errorf("selected page has no follow-up textarea:\n%s", selected)
	}
	if !strings.Contains(selected, `<input type="hidden" name="verb" value="follow-up">`) {
		t.Errorf("selected page has no follow-up form:\n%s", selected)
	}
}

const goldenBoardSelected = "testdata/board_selected.golden.html"

func TestBoardGoldensASelectedRowsDetail(t *testing.T) {
	t.Parallel()

	startedAt := testNow
	now := startedAt.Add(90 * time.Second)
	server := web.NewServer(
		detailStore(t, "testdata/fixtures/run.jsonl", startedAt, now), fixedClock(now), nil, "")

	target := "/board?" + url.Values{"sel": {"https://github.com/o/r/issues/76"}}.Encode()
	assertGolden(t, goldenBoardSelected, []byte(renderPath(t, server, target)))
}

func TestSelectingAnUnknownTicketRendersNothingSelected(t *testing.T) {
	t.Parallel()

	now := testNow
	server := newServer(seededStore(t, now), now)

	rec := get(t, server, selPagePath("sandbox://NOPE"))
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "hx-preserve") {
		t.Errorf("an unmatched ?sel= still rendered a detail row:\n%s", rec.Body)
	}
}

func TestTheDeletedDetailRouteIs404(t *testing.T) {
	t.Parallel()

	now := testNow
	server := newServer(seededStore(t, now), now)

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

	startedAt := testNow
	now := startedAt.Add(90 * time.Second)
	server := newServer(detailStore(t, writeLog(t, 3), startedAt, now), now)

	board := get(t, server, "/")
	selected := httptest.NewRecorder()
	server.ServeHTTP(selected, httptest.NewRequest(
		http.MethodGet, selPagePath("https://github.com/o/r/issues/76"), nil))

	for _, shared := range []string{"1m30s"} {
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

	now := testNow
	server := newServer(detailStore(t, writeLog(t, 1), now, now), now)

	rec := get(t, server, "/")
	body := rec.Body.String()

	want := `hx-get="/board?` + url.Values{"sel": {"https://github.com/o/r/issues/76"}}.Encode() + `"`
	if !strings.Contains(body, want) {
		t.Fatalf("board is missing %s:\n%s", want, body)
	}
}

func TestHTMXIsServedFromTheBinary(t *testing.T) {
	t.Parallel()

	now := testNow
	server := newServer(seededStore(t, now), now)

	rec := get(t, server, "/assets/htmx.min.js")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /assets/htmx.min.js = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "htmx") {
		t.Error("/assets/htmx.min.js does not serve htmx")
	}

	page := get(t, server, "/")
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

	now := testNow
	server := newServer(seededStore(t, now), now)

	rec := get(t, server, "/")
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

func TestOnlyTheSelectedRowCarriesADetailRow(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	tickets := []storepkg.Ticket{
		{URL: "sandbox://A", Repo: "repo", Branch: "a"},
		{URL: "sandbox://B", Repo: "repo", Branch: "b"},
		{URL: "sandbox://C", Repo: "repo", Branch: "c"},
	}
	if err := store.UpsertTickets(ctx, tickets); err != nil {
		t.Fatal(err)
	}
	now := testNow
	if err := store.SaveObservation(ctx, plan.Observation{ObservedAt: now}); err != nil {
		t.Fatal(err)
	}
	server := web.NewServer(store, fixedClock(now), []config.Repo{{Name: "repo"}}, "")
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
				id := detailIDFor(t, server, url)
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

func detailIDFor(t *testing.T, server *web.Server, ticketURL string) string {
	t.Helper()
	rec := get(t, server, selPagePath(ticketURL))
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

	now := testNow
	server := newServer(detailStore(t, writeLog(t, 1), now, now), now)
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
	store := openStore(t)
	tickets := []storepkg.Ticket{
		{URL: "sandbox://A", Repo: "repo", Branch: "a"},
		{URL: "sandbox://B", Repo: "repo", Branch: "b"},
	}
	if err := store.UpsertTickets(ctx, tickets); err != nil {
		t.Fatal(err)
	}
	now := testNow
	if err := store.SaveObservation(ctx, plan.Observation{ObservedAt: now}); err != nil {
		t.Fatal(err)
	}
	server := web.NewServer(store, fixedClock(now), []config.Repo{{Name: "repo"}}, "")

	first := get(t, server, selPagePath("sandbox://A"))
	firstID := detailRowID(t, first.Body.String())

	second := get(t, server, selPagePath("sandbox://B"))
	body := second.Body.String()

	if strings.Contains(body, firstID) {
		t.Errorf("selecting sandbox://B still carries sandbox://A's detail row %q:\n%s", firstID, body)
	}
	if strings.Count(body, `hx-preserve="true"`) != 1 {
		t.Errorf("board carries more than one detail row:\n%s", body)
	}
}

func TestDetailSpansEveryBoardColumn(t *testing.T) {
	t.Parallel()

	now := testNow
	server := newServer(detailStore(t, writeLog(t, 1), now, now), now)

	board := get(t, server, "/")
	columns := strings.Count(board.Body.String(), "<th>")

	selected := httptest.NewRecorder()
	server.ServeHTTP(selected, httptest.NewRequest(
		http.MethodGet, selPagePath("https://github.com/o/r/issues/76"), nil))

	want := fmt.Sprintf(`colspan="%d"`, columns)
	if !strings.Contains(selected.Body.String(), want) {
		t.Errorf("the board has %d columns; the detail fragment does not carry %s", columns, want)
	}
}

var hxAttrRE = regexp.MustCompile(`<(\w+)([^>]*\shx-[\w:-]+=[^>]*)>`)

func TestVerbsNeedNoJavaScript(t *testing.T) {
	t.Parallel()

	states := make([]plan.State, 0, int(plan.RefreshConflicted)+1)
	for s := plan.Blocked; s <= plan.RefreshConflicted; s++ {
		states = append(states, s)
	}
	board, err := web.RenderStatesPage(states)
	if err != nil {
		t.Fatal(err)
	}

	for _, m := range hxAttrRE.FindAllStringSubmatch(board, -1) {
		tag, attrs := m[1], m[2]
		if tag == "table" || tag == "div" || tag == "tr" || tag == "section" {
			continue
		}
		if (tag == "input" || tag == "button") && strings.Contains(attrs, `hx-target="#board"`) {
			continue
		}
		if tag == "form" && (strings.Contains(attrs, `method="post" action="/verb"`) ||
			strings.Contains(attrs, `method="post" action="/launch/open"`)) {
			continue
		}
		t.Errorf("a verb control carries htmx and so needs JavaScript: <%s%s>", tag, attrs)
	}

	for _, want := range []string{
		`<form method="post" action="/verb" hx-post=`,
		`<form id="launch" method="post" action="/launch/open" hx-post="/launch/open"`,
		`<input type="checkbox" form="launch" name="ticket"`,
	} {
		if !strings.Contains(board, want) {
			t.Errorf("the board is missing the scriptless path %q", want)
		}
	}
	for _, m := range hxAttrRE.FindAllStringSubmatch(board, -1) {
		attrs := m[2]
		postsOnItsOwn := strings.Contains(attrs, `method="post" action="/verb"`) ||
			strings.Contains(attrs, `method="post" action="/launch/open"`)
		if strings.Contains(attrs, "hx-post") && !postsOnItsOwn {
			t.Errorf("a verb posts over htmx rather than a form: <%s%s>", m[1], attrs)
		}
	}
}

func selLogPagePath(ticketURL, mode string) string {
	return "/board?" + url.Values{"sel": {ticketURL}, "log": {mode}}.Encode()
}

func TestLogFilterIsAURLParameterActiveInItsOwnLink(t *testing.T) {
	t.Parallel()

	now := testNow
	ticket := "https://github.com/o/r/issues/76"
	server := newServer(detailStore(t, writeLog(t, 3), now, now), now)

	for _, mode := range []string{"all", "skills", "tools", "fails"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, selLogPagePath(ticket, mode), nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("GET log=%s = %d, want 200: %s", mode, rec.Code, rec.Body)
			}
			if want := `aria-current="true">` + mode + `</a>`; !strings.Contains(rec.Body.String(), want) {
				t.Errorf("log=%s does not mark its own filter link active:\n%s", mode, rec.Body.String())
			}
		})
	}
}

func TestLogFilterSurvivesABoardSwap(t *testing.T) {
	t.Parallel()

	now := testNow
	ticket := "https://github.com/o/r/issues/76"
	server := newServer(detailStore(t, writeLog(t, 3), now, now), now)

	rec := get(t, server, selLogPagePath(ticket, "fails"))
	body := rec.Body.String()

	if !strings.Contains(body, `hx-get="/board?log=fails&amp;sel=`) {
		t.Errorf("the board's own poll does not carry log=fails forward:\n%s", body)
	}
}

func TestJumpToFirstFailureIsAPlainAnchor(t *testing.T) {
	t.Parallel()

	now := testNow
	logPath := web.WriteRunLog(t, web.ReadTestdata("run_with_failure.jsonl"))
	ticket := "https://github.com/o/r/issues/76"
	server := newServer(detailStore(t, logPath, now, now), now)

	rec := get(t, server, selPagePath(ticket))
	body := rec.Body.String()

	if !strings.Contains(body, `<a href="#first-fail"`) || !strings.Contains(body, `>first failure</a>`) {
		t.Errorf("no plain anchor jumps to the first failure:\n%s", body)
	}
	if !strings.Contains(body, `id="first-fail"`) {
		t.Errorf("no line carries the id the jump anchor targets:\n%s", body)
	}
}

func TestDroppedKindsNeverReachTheRender(t *testing.T) {
	t.Parallel()

	now := testNow
	logPath := web.WriteRunLog(t, web.ReadTestdata("dropped_kinds.jsonl"))
	ticket := "https://github.com/o/r/issues/76"
	server := newServer(detailStore(t, logPath, now, now), now)

	rec := get(t, server, selPagePath(ticket))
	body := rec.Body.String()

	for _, dropped := range []string{"deadbeef-session", "pondering-deeply"} {
		if strings.Contains(body, dropped) {
			t.Errorf("a dropped kind's raw text reached the render (%q):\n%s", dropped, body)
		}
	}
	if !strings.Contains(body, "echo hi") {
		t.Errorf("the one real event was dropped along with the unrenderable ones:\n%s", body)
	}
	if !strings.Contains(body, "4 lines") {
		t.Errorf("the line count does not cover the dropped lines too:\n%s", body)
	}
}

func TestBoardPollIntervalComesFromTheServer(t *testing.T) {
	t.Parallel()

	now := testNow
	server := newServer(seededStore(t, now), now)
	if got := boardBody(t, server); !strings.Contains(got, `hx-trigger="every 5s"`) {
		t.Errorf("default board does not poll every 5s:\n%s", got)
	}

	server.SetBoardPollSeconds(1)
	if got := boardBody(t, server); !strings.Contains(got, `hx-trigger="every 1s"`) {
		t.Errorf("board does not poll every 1s:\n%s", got)
	}
}

func boardBody(t *testing.T, server *web.Server) string {
	t.Helper()
	rec := get(t, server, "/board")
	return rec.Body.String()
}
