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

	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
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

func sessionPagePath(ticketURL string) string { return view.SessionPath(ticketURL) }

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

	page := get(t, server, "/tickets")
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

	rec := get(t, server, "/tickets")
	body := rec.Body.String()

	if strings.Contains(body, "http-equiv=\"refresh\"") {
		t.Error("the meta refresh is still on the page")
	}
	for _, want := range []string{
		`<table id="board" hx-get="/board?all=1" hx-trigger="every 5s" hx-swap="outerHTML">`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page is missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, `hx-select="#board"`) {
		t.Errorf("the poll re-renders the whole document to select the board back out of it:\n%s", body)
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
		if tag == "table" || tag == "div" || tag == "tr" || tag == "section" ||
			(tag == "a" && !strings.Contains(attrs, "hx-post")) {
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

func TestDroppedKindsNeverReachTheRender(t *testing.T) {
	t.Parallel()

	now := testNow
	logPath := web.WriteRunLog(t, web.ReadTestdata("dropped_kinds.jsonl"))
	ticket := "https://github.com/o/r/issues/76"
	server := newServer(detailStore(t, logPath, now, now), now)

	rec := get(t, server, sessionPagePath(ticket))
	body := rec.Body.String()

	for _, dropped := range []string{"deadbeef-session", "pondering-deeply"} {
		if strings.Contains(body, dropped) {
			t.Errorf("a dropped kind's raw text reached the render (%q):\n%s", dropped, body)
		}
	}
	if !strings.Contains(body, "echo hi") {
		t.Errorf("the one real event was dropped along with the unrenderable ones:\n%s", body)
	}
	if !strings.Contains(body, "lines 4") {
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
